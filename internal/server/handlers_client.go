package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/TriptoAfsin/printty/internal/printer"
	"github.com/TriptoAfsin/printty/internal/store"
)

func (s *Server) needViewer(w http.ResponseWriter, r *http.Request) (viewer, bool) {
	if s.setup != "" {
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
		return viewer{}, false
	}
	v := s.who(r)
	if !v.Known() {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
		return v, false
	}
	return v, true
}

func (s *Server) owns(v viewer, j store.Job) bool {
	if v.User != nil {
		return j.OwnerID == v.User.ID
	}
	return v.GuestKey != "" && j.GuestKey == v.GuestKey && j.OwnerID == ""
}

type printData struct {
	Draft    *store.Job
	Caps     printer.Caps
	Papers   []string
	Previews []int // pages of the draft with a preview image
	Pages    []int // every page of the draft, for the editor
	Guest    bool
	Note     string
	Margins  bool // offer a binding margin (ownLayout)
}

// ownLayout reports whether pages print where Printty's layout puts them,
// so a binding margin can be offered: always on Windows (it draws every
// page itself); on CUPS unless the owner chose to let CUPS lay pages out.
func ownLayout(set store.Settings) bool {
	return runtime.GOOS == "windows" || !set.SystemLayout
}

func (s *Server) printPage(r *http.Request, v viewer, draft *store.Job) page {
	caps := printer.Caps{Color: true, Papers: []string{"A4", "Letter"}, DefaultPaper: "A4"}
	name := s.q.Printer(r.Context())
	if ps, err := s.be.Printers(r.Context()); err == nil {
		for _, p := range ps {
			if p.Name == name {
				caps = p.Caps
			}
		}
	}
	caps.AutoDuplex = s.st.Settings().AutoDuplex
	papers := caps.Papers
	if len(papers) > 4 {
		papers = papers[:4]
	}
	d := printData{Draft: draft, Caps: caps, Papers: papers, Guest: v.User == nil, Previews: s.previewPages(draft), Margins: ownLayout(s.st.Settings())}
	if draft != nil {
		for p := 1; p <= draft.DocPages && p <= 500; p++ {
			d.Pages = append(d.Pages, p)
		}
	}
	return page{Title: "Print", Nav: "print", V: v, Data: d}
}

func (s *Server) getPrint(w http.ResponseWriter, r *http.Request) {
	v, ok := s.needViewer(w, r)
	if !ok {
		return
	}
	var draft *store.Job
	if id := r.URL.Query().Get("draft"); id != "" {
		claim := r.URL.Query().Get("claim")
		j, err := s.st.UpdateJob(id, func(j *store.Job) bool {
			if j.State != store.JobDraft {
				return false
			}
			if j.OwnerID == "" && j.GuestKey == "share:"+claim && claim != "" {
				if v.User != nil {
					j.OwnerID, j.GuestKey = v.User.ID, ""
				} else {
					j.GuestKey, j.GuestName = v.GuestKey, v.GuestName
				}
				return true
			}
			return false
		})
		if err == nil && j.State == store.JobDraft && s.owns(v, j) {
			draft = &j
		}
	}
	s.render(w, r, "print", s.printPage(r, v, draft))
}

// parseOptions reads and validates the print options from form fields.
func parseOptions(f map[string]string, docPages int, caps printer.Caps, margins bool) (printer.Options, error) {
	papers := caps.Papers
	o := printer.Options{Copies: 1, Duplex: printer.Simplex, Color: true, Paper: "A4"}
	if c, err := strconv.Atoi(f["copies"]); err == nil {
		o.Copies = min(max(c, 1), 20)
	}
	if f["pagemode"] == "range" {
		spec := strings.TrimSpace(f["pages"])
		if _, err := printer.ParsePages(spec, docPages); err != nil {
			return o, errors.New("Check the page range, e.g. 1-3, 5")
		}
		o.Pages = spec
	}
	switch printer.Duplex(f["duplex"]) {
	case printer.LongEdge:
		o.Duplex = printer.LongEdge
	case printer.ShortEdge:
		o.Duplex = printer.ShortEdge
	}
	o.Color = f["color"] != "mono"
	for _, p := range papers {
		if p == f["paper"] {
			o.Paper = p
		}
	}
	if err := parseLayout(f, docPages, caps, margins, &o); err != nil {
		return o, err
	}
	return o, nil
}

func (s *Server) postPrint(w http.ResponseWriter, r *http.Request) {
	v, ok := s.needViewer(w, r)
	if !ok {
		return
	}
	files, fields, err := s.readForm(w, r, false)
	if err != nil {
		s.printError(w, r, v, nil, err.Error())
		return
	}
	var job *store.Job
	if len(files) > 0 {
		j, err := s.makeJob(r.Context(), files)
		if err != nil {
			s.printError(w, r, v, nil, err.Error())
			return
		}
		job = &j
	}
	if job == nil && fields["draft"] != "" {
		if j, err := s.st.Job(fields["draft"]); err == nil && j.State == store.JobDraft && s.owns(v, j) {
			job = &j
		}
	}
	if job == nil {
		s.printError(w, r, v, nil, "Choose a PDF or photo to print.")
		return
	}
	pp := s.printPage(r, v, job)
	pd := pp.Data.(printData)
	opts, err := parseOptions(fields, job.DocPages, pd.Caps, pd.Margins)
	if err != nil {
		s.saveDraft(*job, v)
		s.printError(w, r, v, job, err.Error())
		return
	}
	if v.User == nil {
		set := s.st.Settings()
		if !s.limiter.allow("guest:"+clientIP(r), max(set.GuestPerHour, 1), time.Hour) || !s.limiter.allow("guestkey:"+v.GuestKey, max(set.GuestPerHour, 1), time.Hour) {
			s.saveDraft(*job, v) // expires within the hour, taking the file with it
			s.printError(w, r, v, nil, "Guests can send a few requests an hour. Try again later, or ask the owner for an invite.")
			return
		}
		pages, _ := printer.ParsePages(opts.Pages, job.DocPages)
		if len(pages)*opts.Copies > max(set.GuestMaxPages, 1) {
			s.saveDraft(*job, v)
			s.printError(w, r, v, job, "Guests can print up to "+strconv.Itoa(set.GuestMaxPages)+" pages per request.")
			return
		}
	}
	note := strings.TrimSpace(fields["note"])
	if len([]rune(note)) > 200 {
		note = string([]rune(note)[:200])
	}
	j := *job
	j.Options, j.Note, j.ClientIP = opts, note, clientIP(r)
	if v.User != nil {
		j.OwnerID, j.GuestKey = v.User.ID, ""
	} else {
		j.GuestKey, j.GuestName = v.GuestKey, v.GuestName
	}
	if _, err := s.st.Job(j.ID); err != nil {
		s.st.AddJob(j)
	} else {
		s.st.UpdateJob(j.ID, func(x *store.Job) bool {
			x.Options, x.Note, x.ClientIP, x.OwnerID, x.GuestKey, x.GuestName = j.Options, j.Note, j.ClientIP, j.OwnerID, j.GuestKey, j.GuestName
			return true
		})
	}
	by := ""
	if v.User != nil {
		by = v.User.ID
	}
	j, err = s.q.Submit(r.Context(), j.ID, v.AutoApprove(), by)
	if err != nil {
		s.printError(w, r, v, nil, "Couldn't queue that job.")
		return
	}
	msg := "Sent. The owner has been asked to approve it."
	if j.State == store.JobQueued {
		msg = "Printing now."
	}
	back(w, r, "/queue", msg)
}

// saveDraft keeps an uploaded file as a draft so a form error doesn't lose it.
func (s *Server) saveDraft(j store.Job, v viewer) {
	if _, err := s.st.Job(j.ID); err == nil {
		return
	}
	if v.User != nil {
		j.OwnerID = v.User.ID
	} else {
		j.GuestKey, j.GuestName = v.GuestKey, v.GuestName
	}
	s.st.AddJob(j)
}

func (s *Server) printError(w http.ResponseWriter, r *http.Request, v viewer, draft *store.Job, msg string) {
	p := s.printPage(r, v, draft)
	p.Error = msg
	w.WriteHeader(http.StatusBadRequest)
	s.render(w, r, "print", p)
}

// postShare is the PWA share target. It accepts the file without a session
// (the share sheet may not carry cookies), keeps it as a draft, and hands the
// browser a claim link; nothing prints until a signed-in person or guest confirms.
func (s *Server) postShare(w http.ResponseWriter, r *http.Request) {
	if !s.limiter.allow("share:"+clientIP(r), 20, time.Hour) {
		http.Error(w, "Too many shares. Try again later.", http.StatusTooManyRequests)
		return
	}
	files, _, err := s.readForm(w, r, true)
	if err == nil && len(files) == 0 {
		err = errors.New("Nothing to print was shared.")
	}
	var j store.Job
	if err == nil {
		j, err = s.makeJob(r.Context(), files)
	}
	if err != nil {
		http.Redirect(w, r, "/?shareError="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	claim := store.RandomToken(8)
	j.GuestKey = "share:" + claim
	s.st.AddJob(j)
	http.Redirect(w, r, "/?draft="+j.ID+"&claim="+claim, http.StatusSeeOther)
}

func (s *Server) getQueue(w http.ResponseWriter, r *http.Request) {
	v, ok := s.needViewer(w, r)
	if !ok {
		return
	}
	jobs := s.st.Jobs(func(j store.Job) bool { return s.owns(v, j) && j.State != store.JobDraft })
	if len(jobs) > 20 {
		jobs = jobs[:20]
	}
	s.render(w, r, "queue", page{Title: "Your prints", Nav: "queue", V: v, Data: jobs})
}

func (s *Server) postCancel(w http.ResponseWriter, r *http.Request) {
	v, ok := s.needViewer(w, r)
	if !ok {
		return
	}
	j, err := s.st.Job(r.PathValue("id"))
	if err != nil || (!s.owns(v, j) && !v.IsAdmin()) {
		http.NotFound(w, r)
		return
	}
	if _, err := s.q.Cancel(j.ID); err != nil {
		back(w, r, "/queue", err.Error())
		return
	}
	back(w, r, "/queue", "Cancelled.")
}

func (s *Server) apiStatus(w http.ResponseWriter, r *http.Request) {
	if !s.who(r).Known() {
		http.Error(w, "sign in", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(s.q.Status(r.Context()))
}
