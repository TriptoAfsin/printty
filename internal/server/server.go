// Package server is Printty's HTTP layer: pages, forms, auth and the PWA.
package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/TriptoAfsin/printty/internal/auth"
	"github.com/TriptoAfsin/printty/internal/memtrim"
	"github.com/TriptoAfsin/printty/internal/netinfo"
	"github.com/TriptoAfsin/printty/internal/notify"
	"github.com/TriptoAfsin/printty/internal/printer"
	"github.com/TriptoAfsin/printty/internal/queue"
	"github.com/TriptoAfsin/printty/internal/store"
	"github.com/TriptoAfsin/printty/internal/web"
)

const (
	sessionCookie = "printty_session"
	guestCookie   = "printty_guest"
	sessionMaxAge = 90 * 24 * time.Hour
)

type Server struct {
	st      *store.Store
	q       *queue.Queue
	be      printer.Backend
	tg      *notify.Telegram
	maxUp   int64
	secure  bool
	setup   string // one-time setup code, empty once an admin exists
	pages   map[string]*template.Template
	static  http.Handler
	limiter *limiter
	listen  string // listen address, for the "open on phones" URLs
	rt      Runtime
}

func New(st *store.Store, q *queue.Queue, be printer.Backend, tg *notify.Telegram, maxUploadMB int, secure bool, listen string) (*Server, error) {
	s := &Server{st: st, q: q, be: be, tg: tg, maxUp: int64(maxUploadMB) << 20, secure: secure, limiter: newLimiter(), listen: listen}
	if st.UserCount() == 0 {
		s.setup = strings.ToUpper(store.RandomToken(4))
	}
	if err := s.parseTemplates(); err != nil {
		return nil, err
	}
	sub, _ := fs.Sub(web.Static, "static")
	s.static = http.StripPrefix("/static/", cacheForever(http.FileServer(http.FS(sub))))
	return s, nil
}

// Addresses are the URLs other devices can use, worked out fresh each time
// (the LAN IP can change when the router hands out a new one).
func (s *Server) Addresses() []netinfo.Address { return netinfo.URLs(s.listen, s.secure) }

// SetupCode is printed to the console on first run.
func (s *Server) SetupCode() string { return s.setup }

var assetVersion = func() string {
	h := sha256.New()
	fs.WalkDir(web.Static, "static", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			b, _ := fs.ReadFile(web.Static, p)
			h.Write(b)
		}
		return nil
	})
	return hex.EncodeToString(h.Sum(nil))[:10]
}()

func (s *Server) parseTemplates() error {
	s.pages = map[string]*template.Template{}
	funcs := template.FuncMap{
		// asset returns a static URL with a content fingerprint, so browsers
		// pick up new CSS/JS right after an update despite long caching.
		"asset": func(name string) string { return "/static/" + name + "?v=" + assetVersion },
		"plural": func(n int, w string) string {
			if n == 1 {
				return "1 " + w
			}
			if strings.HasSuffix(w, "y") {
				return fmt.Sprintf("%d %sies", n, strings.TrimSuffix(w, "y"))
			}
			return fmt.Sprintf("%d %ss", n, w)
		},
		"clock": func(t time.Time) string {
			if t.IsZero() {
				return ""
			}
			if time.Since(t) < 20*time.Hour {
				return t.Format("15:04")
			}
			if time.Since(t) < 44*time.Hour {
				return "Yesterday"
			}
			return t.Format("2 Jan")
		},
		"initial": func(s string) string {
			for _, r := range s {
				return strings.ToUpper(string(r))
			}
			return "?"
		},
		"pct": func(a, b int) int {
			if b <= 0 {
				return 0
			}
			return a * 100 / b
		},
	}
	names, err := fs.Glob(web.Templates, "templates/*.html")
	if err != nil {
		return err
	}
	for _, n := range names {
		base := filepath.Base(n)
		if base == "layout.html" {
			continue
		}
		t, err := template.New("").Funcs(funcs).ParseFS(web.Templates, "templates/layout.html", n)
		if err != nil {
			return fmt.Errorf("template %s: %w", base, err)
		}
		s.pages[strings.TrimSuffix(base, ".html")] = t
	}
	return nil
}

// Handler returns the router with security middleware.
func (s *Server) Handler() http.Handler {
	m := http.NewServeMux()
	m.Handle("GET /static/", s.static)
	m.HandleFunc("GET /manifest.webmanifest", s.serveFile("static/manifest.webmanifest", "application/manifest+json"))
	m.HandleFunc("GET /sw.js", s.serveFile("static/sw.js", "text/javascript"))
	m.HandleFunc("GET /favicon.ico", s.serveFile("static/icon.svg", "image/svg+xml"))

	m.HandleFunc("GET /setup", s.getSetup)
	m.HandleFunc("POST /setup", s.postSetup)
	m.HandleFunc("GET /login", s.getLogin)
	m.HandleFunc("POST /login", s.postLogin)
	m.HandleFunc("POST /logout", s.postLogout)
	m.HandleFunc("GET /invite/{token}", s.getInvite)
	m.HandleFunc("POST /invite/{token}", s.postInvite)
	m.HandleFunc("GET /guest", s.getGuest)
	m.HandleFunc("POST /guest", s.postGuest)

	m.HandleFunc("GET /{$}", s.getPrint)
	m.HandleFunc("POST /print", s.postPrint)
	m.HandleFunc("POST /share", s.postShare)
	m.HandleFunc("GET /queue", s.getQueue)
	m.HandleFunc("POST /jobs/{id}/cancel", s.postCancel)
	m.HandleFunc("GET /api/status", s.apiStatus)

	m.HandleFunc("GET /admin", s.admin(s.getAdmin))
	m.HandleFunc("POST /admin/jobs/{id}/{action}", s.admin(s.postJobAction))
	m.HandleFunc("GET /admin/users", s.admin(s.getUsers))
	m.HandleFunc("POST /admin/invites", s.admin(s.postInviteCreate))
	m.HandleFunc("POST /admin/users/{id}/{action}", s.admin(s.postUserAction))
	m.HandleFunc("GET /admin/settings", s.admin(s.getSettings))
	m.HandleFunc("POST /admin/settings", s.admin(s.postSettings))
	m.HandleFunc("POST /admin/test-alert", s.admin(s.postTestAlert))
	m.HandleFunc("POST /admin/selftest", s.admin(s.postSelfTest))
	m.HandleFunc("POST /admin/autostart", s.admin(s.postAutostart))
	m.HandleFunc("POST /admin/service/install", s.admin(s.postServiceInstall))
	m.HandleFunc("POST /admin/service/remove", s.admin(s.postServiceRemove))
	return s.secureHeaders(s.sameOrigin(m))
}

func cacheForever(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=86400")
		h.ServeHTTP(w, r)
	})
}

func (s *Server) serveFile(path, ctype string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		b, err := fs.ReadFile(web.Static, path)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", ctype)
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(b)
	}
}

func (s *Server) secureHeaders(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hd := w.Header()
		hd.Set("X-Content-Type-Options", "nosniff")
		hd.Set("Referrer-Policy", "same-origin")
		hd.Set("X-Frame-Options", "DENY")
		hd.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; frame-ancestors 'none'; form-action 'self'")
		h.ServeHTTP(w, r)
	})
}

// sameOrigin rejects cross-site form posts (CSRF). The share target is
// exempt: it only creates a draft that the person must confirm.
func (s *Server) sameOrigin(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path != "/share" {
			if o := r.Header.Get("Origin"); o != "" && o != "null" {
				u, err := url.Parse(o)
				if err != nil || u.Host != r.Host {
					http.Error(w, "Cross-site request blocked", http.StatusForbidden)
					return
				}
			}
			if sf := r.Header.Get("Sec-Fetch-Site"); sf == "cross-site" {
				http.Error(w, "Cross-site request blocked", http.StatusForbidden)
				return
			}
		}
		h.ServeHTTP(w, r)
	})
}

// ---- identity

type viewer struct {
	User      *store.User
	GuestKey  string
	GuestName string
}

func (v viewer) IsAdmin() bool  { return v.User != nil && v.User.Role == store.RoleAdmin }
func (v viewer) SignedIn() bool { return v.User != nil }
func (v viewer) Known() bool    { return v.User != nil || v.GuestKey != "" }
func (v viewer) AutoApprove() bool {
	return v.User != nil && (v.User.Role == store.RoleAdmin || v.User.Trusted)
}
func (v viewer) DisplayName() string {
	if v.User != nil {
		return v.User.Name
	}
	return v.GuestName
}

func (s *Server) who(r *http.Request) viewer {
	var v viewer
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		if u, err := s.st.SessionUser(auth.HashToken(c.Value), sessionMaxAge); err == nil {
			v.User = &u
			return v
		}
	}
	if c, err := r.Cookie(guestCookie); err == nil && s.st.Settings().GuestsEnabled {
		if key, name, ok := strings.Cut(c.Value, "."); ok && len(key) == 32 {
			if n, err := url.QueryUnescape(name); err == nil {
				v.GuestKey, v.GuestName = key, n
			}
		}
	}
	return v
}

func (s *Server) admin(h func(http.ResponseWriter, *http.Request, viewer)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		v := s.who(r)
		if !v.IsAdmin() {
			if v.SignedIn() {
				http.Error(w, "Admins only", http.StatusForbidden)
				return
			}
			http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.Path), http.StatusSeeOther)
			return
		}
		h(w, r, v)
	}
}

func (s *Server) setCookie(w http.ResponseWriter, name, val string, maxAge time.Duration) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: val, Path: "/", HttpOnly: true, Secure: s.secure,
		SameSite: http.SameSiteLaxMode, MaxAge: int(maxAge.Seconds())})
}

func (s *Server) startSession(w http.ResponseWriter, u store.User) error {
	tok := store.RandomToken(32)
	if err := s.st.AddSession(store.Session{TokenHash: auth.HashToken(tok), UserID: u.ID, CreatedAt: time.Now(), LastSeen: time.Now()}); err != nil {
		return err
	}
	s.setCookie(w, sessionCookie, tok, sessionMaxAge)
	s.setCookie(w, guestCookie, "", -time.Second)
	return nil
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ---- rendering

type page struct {
	Title    string
	Nav      string // which tab is current
	V        viewer
	Status   printer.Status
	Printer  string
	Flash    string
	Error    string
	Data     any
	Admin    bool
	Requests int
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, name string, p page) {
	t, ok := s.pages[name]
	if !ok {
		http.Error(w, "missing page", 500)
		return
	}
	if p.V.User == nil && p.V.GuestKey == "" {
		p.V = s.who(r)
	}
	p.Admin = p.V.IsAdmin()
	if p.Admin {
		p.Requests = len(s.st.Jobs(func(j store.Job) bool { return j.State == store.JobPending }))
	}
	if p.Status.State == "" && name != "login" && name != "setup" && name != "invite" && name != "guest" {
		p.Status = s.q.Status(r.Context())
		p.Printer = s.q.Printer(r.Context())
	}
	if f := r.URL.Query().Get("ok"); f != "" && p.Flash == "" {
		p.Flash = f
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := t.ExecuteTemplate(w, "layout", p); err != nil {
		slog.Error("render", "page", name, "err", err)
	}
}

func back(w http.ResponseWriter, r *http.Request, path, msg string) {
	if msg != "" {
		sep := "?"
		if strings.Contains(path, "?") {
			sep = "&"
		}
		path += sep + "ok=" + url.QueryEscape(msg)
	}
	http.Redirect(w, r, path, http.StatusSeeOther)
}

// ---- uploads

// sniff returns pdf, png or jpeg from the first bytes, or "".
func sniff(head []byte) string {
	switch {
	case len(head) >= 5 && string(head[:5]) == "%PDF-":
		return "pdf"
	case len(head) >= 8 && string(head[:8]) == "\x89PNG\r\n\x1a\n":
		return "png"
	case len(head) >= 3 && head[0] == 0xFF && head[1] == 0xD8 && head[2] == 0xFF:
		return "jpeg"
	}
	return ""
}

var errBadFile = errors.New("Printty prints PDF, JPG and PNG files")

// saveUpload stores the uploaded file under a random name and returns a draft job.
func (s *Server) saveUpload(ctx context.Context, src io.Reader, filename string) (store.Job, error) {
	head := make([]byte, 512)
	n, _ := io.ReadFull(src, head)
	head = head[:n]
	kind := sniff(head)
	if kind == "" {
		return store.Job{}, errBadFile
	}
	id := store.NewID()
	stored := id + "." + map[string]string{"pdf": "pdf", "png": "png", "jpeg": "jpg"}[kind]
	path := filepath.Join(s.st.FilesDir(), stored)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return store.Job{}, err
	}
	size, err := io.Copy(f, io.MultiReader(strings.NewReader(string(head)), io.LimitReader(src, s.maxUp)))
	f.Close()
	if err != nil {
		os.Remove(path)
		return store.Job{}, fmt.Errorf("upload failed")
	}
	if size > s.maxUp {
		os.Remove(path)
		return store.Job{}, fmt.Errorf("That file is over %d MB", s.maxUp>>20)
	}
	pages, err := s.be.PageCount(ctx, path, kind)
	memtrim.Now()
	if err != nil || pages < 1 {
		os.Remove(path)
		return store.Job{}, fmt.Errorf("That PDF couldn't be opened. Is it password-protected or damaged?")
	}
	name := filepath.Base(strings.ReplaceAll(filename, "\\", "/"))
	if name == "" || name == "." {
		name = "document." + kind
	}
	return store.Job{ID: id, FileName: name, Kind: kind, StoredAs: stored, Size: size, DocPages: pages,
		State: store.JobDraft, CreatedAt: time.Now(), UpdatedAt: time.Now()}, nil
}

// ---- rate limiting (guests and logins)

type limiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

func newLimiter() *limiter { return &limiter{hits: map[string][]time.Time{}} }

// allow reports whether key has fewer than n hits within window, and records one.
func (l *limiter) allow(key string, n int, window time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	keep := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if now.Sub(t) < window {
			keep = append(keep, t)
		}
	}
	if len(keep) >= n {
		l.hits[key] = keep
		return false
	}
	l.hits[key] = append(keep, now)
	if len(l.hits) > 1000 { // bound memory
		for k, v := range l.hits {
			if len(v) == 0 || now.Sub(v[len(v)-1]) > window {
				delete(l.hits, k)
			}
		}
	}
	return true
}
