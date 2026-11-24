package server

import (
	"crypto/subtle"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/TriptoAfsin/printty/internal/auth"
	"github.com/TriptoAfsin/printty/internal/store"
)

func cleanUsername(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func safeNext(next string) string {
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		return "/"
	}
	return next
}

// ---- first-run setup

func (s *Server) getSetup(w http.ResponseWriter, r *http.Request) {
	if s.setup == "" {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	s.render(w, r, "setup", page{Title: "Set up Printty"})
}

func (s *Server) postSetup(w http.ResponseWriter, r *http.Request) {
	if s.setup == "" || s.st.UserCount() > 0 {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if !s.limiter.allow("setup:"+clientIP(r), 10, time.Hour) {
		s.render(w, r, "setup", page{Title: "Set up Printty", Error: "Too many attempts. Try again later."})
		return
	}
	code := strings.ToUpper(strings.TrimSpace(r.FormValue("code")))
	if subtle.ConstantTimeCompare([]byte(code), []byte(s.setup)) != 1 {
		s.render(w, r, "setup", page{Title: "Set up Printty", Error: "That setup code is wrong. It's shown in the window where Printty is running."})
		return
	}
	u, err := s.newUser(r, store.RoleAdmin)
	if err != nil {
		s.render(w, r, "setup", page{Title: "Set up Printty", Error: err.Error()})
		return
	}
	s.setup = ""
	s.startSession(w, u)
	back(w, r, "/admin", "You're set up. Invite people from Users.")
}

func (s *Server) newUser(r *http.Request, role store.Role) (store.User, error) {
	name := strings.TrimSpace(r.FormValue("name"))
	username := cleanUsername(r.FormValue("username"))
	if name == "" {
		name = username
	}
	if len(username) < 2 {
		return store.User{}, errorf("Pick a username with at least 2 letters or numbers.")
	}
	if r.FormValue("password") != r.FormValue("password2") {
		return store.User{}, errorf("The two passwords don't match.")
	}
	hash, err := auth.HashPassword(r.FormValue("password"))
	if err != nil {
		return store.User{}, errorf("Passwords need at least 8 characters.")
	}
	u := store.User{ID: store.NewID(), Username: username, Name: name, PassHash: hash, Role: role, CreatedAt: time.Now()}
	if err := s.st.AddUser(u); err != nil {
		return store.User{}, errorf("That username is taken.")
	}
	return u, nil
}

type errorf string

func (e errorf) Error() string { return string(e) }

// ---- sign in

func (s *Server) getLogin(w http.ResponseWriter, r *http.Request) {
	if s.setup != "" {
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
		return
	}
	if s.who(r).SignedIn() {
		http.Redirect(w, r, safeNext(r.URL.Query().Get("next")), http.StatusSeeOther)
		return
	}
	s.render(w, r, "login", page{Title: "Sign in", Data: map[string]any{"Next": r.URL.Query().Get("next"), "Guests": s.st.Settings().GuestsEnabled}})
}

func (s *Server) postLogin(w http.ResponseWriter, r *http.Request) {
	data := map[string]any{"Next": r.FormValue("next"), "Guests": s.st.Settings().GuestsEnabled, "Username": r.FormValue("username")}
	if !s.limiter.allow("login:"+clientIP(r), 10, 15*time.Minute) {
		s.render(w, r, "login", page{Title: "Sign in", Error: "Too many attempts. Wait 15 minutes and try again.", Data: data})
		return
	}
	u, err := s.st.UserByName(cleanUsername(r.FormValue("username")))
	if err != nil || u.Disabled || !auth.CheckPassword(u.PassHash, r.FormValue("password")) {
		s.render(w, r, "login", page{Title: "Sign in", Error: "That username and password don't match.", Data: data})
		return
	}
	if err := s.startSession(w, u); err != nil {
		http.Error(w, "Couldn't sign in", 500)
		return
	}
	next := safeNext(r.FormValue("next"))
	if next == "/" && u.Role == store.RoleAdmin {
		next = "/admin"
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (s *Server) postLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.st.DeleteSession(auth.HashToken(c.Value))
	}
	s.setCookie(w, sessionCookie, "", -time.Second)
	s.setCookie(w, guestCookie, "", -time.Second)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// ---- invites

func (s *Server) getInvite(w http.ResponseWriter, r *http.Request) {
	ok := s.st.ValidInvite(auth.HashToken(r.PathValue("token")))
	s.render(w, r, "invite", page{Title: "Join Printty", Data: map[string]any{"Valid": ok, "Token": r.PathValue("token")}})
}

func (s *Server) postInvite(w http.ResponseWriter, r *http.Request) {
	tok := r.PathValue("token")
	th := auth.HashToken(tok)
	data := map[string]any{"Valid": true, "Token": tok, "Name": r.FormValue("name"), "Username": r.FormValue("username")}
	if !s.st.ValidInvite(th) {
		data["Valid"] = false
		s.render(w, r, "invite", page{Title: "Join Printty", Data: data})
		return
	}
	u, err := s.newUser(r, store.RoleMember)
	if err != nil {
		s.render(w, r, "invite", page{Title: "Join Printty", Error: err.Error(), Data: data})
		return
	}
	s.st.UseInvite(th, u.ID)
	s.startSession(w, u)
	back(w, r, "/", "Welcome, "+u.Name+". You can print now.")
}

// ---- guests

func (s *Server) getGuest(w http.ResponseWriter, r *http.Request) {
	if !s.st.Settings().GuestsEnabled {
		s.render(w, r, "guest", page{Title: "Ask as a guest", Data: map[string]any{"Off": true}})
		return
	}
	s.render(w, r, "guest", page{Title: "Ask as a guest", Data: map[string]any{"Next": r.URL.Query().Get("next")}})
}

func (s *Server) postGuest(w http.ResponseWriter, r *http.Request) {
	if !s.st.Settings().GuestsEnabled {
		http.Redirect(w, r, "/guest", http.StatusSeeOther)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if len([]rune(name)) > 40 {
		name = string([]rune(name)[:40])
	}
	if name == "" {
		s.render(w, r, "guest", page{Title: "Ask as a guest", Error: "Tell the owner who you are.", Data: map[string]any{}})
		return
	}
	key := store.RandomToken(16)
	if v := s.who(r); v.GuestKey != "" {
		key = v.GuestKey // keep their queue across renames
	}
	s.setCookie(w, guestCookie, key+"."+url.QueryEscape(name), 30*24*time.Hour)
	http.Redirect(w, r, safeNext(r.FormValue("next")), http.StatusSeeOther)
}
