package server

import (
	"net/http"
	"time"

	"github.com/TriptoAfsin/printty/internal/auth"
	"github.com/TriptoAfsin/printty/internal/store"
)

const resetTTL = 24 * time.Hour

// postResetLink makes a one-time password-reset link for a user (admin only).
// The admin sends it to them; it works once, for 24 hours.
func (s *Server) postResetLink(w http.ResponseWriter, r *http.Request, v viewer) {
	u, err := s.st.User(r.PathValue("id"))
	if err != nil {
		back(w, r, "/admin/users", "That person isn't here any more.")
		return
	}
	if u.ID == v.User.ID {
		http.Redirect(w, r, "/account", http.StatusSeeOther)
		return
	}
	tok := store.RandomToken(16)
	if err := s.st.AddReset(auth.HashToken(tok), u.ID, v.User.ID, resetTTL); err != nil {
		back(w, r, "/admin/users", "Couldn't make a reset link.")
		return
	}
	pg := s.usersPage(r, v, "")
	d := pg.Data.(usersData)
	d.ResetLink, d.ResetName = s.baseURL(r)+"/reset/"+tok, u.Name
	pg.Data = d
	s.render(w, r, "users", pg)
}

// getReset shows the "choose a new password" form for a reset link.
func (s *Server) getReset(w http.ResponseWriter, r *http.Request) {
	_, ok := s.st.ResetUser(auth.HashToken(r.PathValue("token")))
	s.render(w, r, "reset", page{Title: "Choose a new password", Data: map[string]any{"Valid": ok, "Token": r.PathValue("token")}})
}

func (s *Server) postReset(w http.ResponseWriter, r *http.Request) {
	tok := r.PathValue("token")
	data := map[string]any{"Valid": true, "Token": tok}
	if !s.limiter.allow("reset:"+clientIP(r), 10, time.Hour) {
		s.render(w, r, "reset", page{Title: "Choose a new password", Error: "Too many attempts. Try again later.", Data: data})
		return
	}
	th := auth.HashToken(tok)
	uid, ok := s.st.ResetUser(th)
	if !ok {
		data["Valid"] = false
		s.render(w, r, "reset", page{Title: "Choose a new password", Data: data})
		return
	}
	hash, msg := newPasswordHash(r)
	if msg != "" {
		s.render(w, r, "reset", page{Title: "Choose a new password", Error: msg, Data: data})
		return
	}
	s.st.UpdateUser(uid, func(u *store.User) { u.PassHash = hash })
	s.st.UseReset(th)
	s.st.DeleteUserSessions(uid) // sign out everywhere with the old password
	u, err := s.st.User(uid)
	if err != nil || u.Disabled {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	s.startSession(w, u)
	next := "/"
	if u.Role == store.RoleAdmin {
		next = "/admin"
	}
	back(w, r, next, "Password changed. You're signed in.")
}

// newPasswordHash checks password and password2 and hashes them.
func newPasswordHash(r *http.Request) (string, string) {
	if r.FormValue("password") != r.FormValue("password2") {
		return "", "The two passwords don't match."
	}
	hash, err := auth.HashPassword(r.FormValue("password"))
	if err != nil {
		return "", "Passwords need at least 8 characters."
	}
	return hash, ""
}

// getAccount lets a signed-in person change their own password.
func (s *Server) getAccount(w http.ResponseWriter, r *http.Request) {
	v, ok := s.needViewer(w, r)
	if !ok {
		return
	}
	if v.User == nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	nav := "print"
	if v.IsAdmin() {
		nav = "settings"
	}
	s.render(w, r, "account", page{Title: "Your account", Nav: nav, V: v})
}

func (s *Server) postAccount(w http.ResponseWriter, r *http.Request) {
	v, ok := s.needViewer(w, r)
	if !ok {
		return
	}
	if v.User == nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	nav := "print"
	if v.IsAdmin() {
		nav = "settings"
	}
	fail := func(msg string) {
		s.render(w, r, "account", page{Title: "Your account", Nav: nav, V: v, Error: msg})
	}
	if !s.limiter.allow("account:"+v.User.ID, 10, 15*time.Minute) {
		fail("Too many attempts. Wait 15 minutes and try again.")
		return
	}
	if !auth.CheckPassword(v.User.PassHash, r.FormValue("current")) {
		fail("Your current password isn't right.")
		return
	}
	hash, msg := newPasswordHash(r)
	if msg != "" {
		fail(msg)
		return
	}
	s.st.UpdateUser(v.User.ID, func(u *store.User) { u.PassHash = hash })
	// Other devices are signed out; this one gets a fresh session.
	s.st.DeleteUserSessions(v.User.ID)
	if u, err := s.st.User(v.User.ID); err == nil {
		s.startSession(w, u)
	}
	back(w, r, "/account", "Password changed. Other devices were signed out.")
}
