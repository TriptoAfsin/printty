package server

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/TriptoAfsin/printty/internal/auth"
	"github.com/TriptoAfsin/printty/internal/netinfo"
	"github.com/TriptoAfsin/printty/internal/printer"
	"github.com/TriptoAfsin/printty/internal/store"
)

type jobView struct {
	store.Job
	Who   string
	Role  string
	Pages int // pages that will print (per copy)
}

func (s *Server) view(j store.Job) jobView {
	jv := jobView{Job: j, Who: j.GuestName, Role: "Guest"}
	if j.OwnerID != "" {
		if u, err := s.st.User(j.OwnerID); err == nil {
			jv.Who, jv.Role = u.Name, "Member"
			if u.Role == store.RoleAdmin {
				jv.Role = "Admin"
			} else if u.Trusted {
				jv.Role = "Trusted"
			}
		}
	}
	if p, err := printer.ParsePages(j.Options.Pages, max(j.DocPages, 1)); err == nil {
		jv.Pages = len(p)
	}
	return jv
}

type adminData struct {
	Requests []jobView
	Active   []jobView
	Recent   []jobView
	Telegram bool
	Links    bool // Telegram buttons are links because another program owns the bot
	Settings store.Settings
	URLs     []netinfo.Address
}

func (s *Server) getAdmin(w http.ResponseWriter, r *http.Request, v viewer) {
	d := adminData{Telegram: s.tg != nil, Links: s.tg != nil && !s.tg.CallbacksWork(), Settings: s.st.Settings(), URLs: s.Addresses()}
	for _, j := range s.st.Jobs(nil) {
		switch {
		case j.State == store.JobPending:
			d.Requests = append([]jobView{s.view(j)}, d.Requests...) // oldest first
		case j.State == store.JobQueued || j.State == store.JobPrinting || j.State == store.JobHeld || j.State == store.JobFlip:
			d.Active = append(d.Active, s.view(j))
		case j.State.Finished() && len(d.Recent) < 8:
			d.Recent = append(d.Recent, s.view(j))
		}
	}
	s.render(w, r, "admin", page{Title: "Requests", Nav: "requests", V: v, Data: d})
}

func (s *Server) postJobAction(w http.ResponseWriter, r *http.Request, v viewer) {
	id := r.PathValue("id")
	var err error
	msg := ""
	switch r.PathValue("action") {
	case "approve":
		_, err = s.q.Approve(id, v.User.ID)
		msg = "Approved. Printing now."
	case "reject":
		_, err = s.q.Reject(id, v.User.ID, r.FormValue("reason"))
		msg = "Rejected."
	case "continue":
		_, err = s.q.ContinueFlip(id)
		msg = "Printing the back sides."
	case "cancel":
		_, err = s.q.Cancel(id)
		msg = "Cancelled."
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		msg = err.Error()
	}
	back(w, r, "/admin", msg)
}

type userRow struct {
	store.User
	Sessions int
}

type usersData struct {
	Users    []userRow
	Invites  int
	NewLink  string
	Settings store.Settings
}

func (s *Server) usersPage(r *http.Request, v viewer, link string) page {
	d := usersData{Invites: len(s.st.PendingInvites()), NewLink: link, Settings: s.st.Settings()}
	for _, u := range s.st.Users() {
		d.Users = append(d.Users, userRow{User: u, Sessions: s.st.SessionCount(u.ID)})
	}
	return page{Title: "Users", Nav: "users", V: v, Data: d}
}

func (s *Server) getUsers(w http.ResponseWriter, r *http.Request, v viewer) {
	s.render(w, r, "users", s.usersPage(r, v, ""))
}

// baseURL is the address to put in links for other devices. When the admin
// is on this PC (localhost), use the LAN address instead, so phones can open it.
func (s *Server) baseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	host, _, err := net.SplitHostPort(r.Host)
	if err != nil {
		host = r.Host
	}
	if ip := net.ParseIP(host); host == "localhost" || (ip != nil && ip.IsLoopback()) {
		if p := netinfo.Primary(s.Addresses()); p != "" {
			return p
		}
	}
	return scheme + "://" + r.Host
}

func (s *Server) postInviteCreate(w http.ResponseWriter, r *http.Request, v viewer) {
	tok := store.RandomToken(16)
	s.st.AddInvite(store.Invite{TokenHash: auth.HashToken(tok), CreatedBy: v.User.ID, Note: r.FormValue("note"), Expires: time.Now().Add(7 * 24 * time.Hour)})
	s.render(w, r, "users", s.usersPage(r, v, s.baseURL(r)+"/invite/"+tok))
}

func (s *Server) postUserAction(w http.ResponseWriter, r *http.Request, v viewer) {
	id := r.PathValue("id")
	if id == v.User.ID {
		back(w, r, "/admin/users", "You can't change your own account here.")
		return
	}
	var err error
	msg := "Saved."
	switch r.PathValue("action") {
	case "trust":
		err = s.st.UpdateUser(id, func(u *store.User) { u.Trusted = true })
		msg = "Their prints no longer need approval."
	case "untrust":
		err = s.st.UpdateUser(id, func(u *store.User) { u.Trusted = false })
		msg = "Their prints need approval again."
	case "disable":
		err = s.st.UpdateUser(id, func(u *store.User) { u.Disabled = true })
		s.st.DeleteUserSessions(id)
		msg = "Access removed and signed out everywhere."
	case "enable":
		err = s.st.UpdateUser(id, func(u *store.User) { u.Disabled = false })
		msg = "Access restored."
	case "signout":
		err = s.st.DeleteUserSessions(id)
		msg = "Signed out on all devices."
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		msg = "That user wasn't found."
	}
	back(w, r, "/admin/users", msg)
}

type settingsData struct {
	Settings store.Settings
	Printers []printer.Info
	Current  string
	Telegram bool
	Links    bool
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request, v viewer) {
	ps, _ := s.be.Printers(r.Context())
	d := settingsData{Settings: s.st.Settings(), Printers: ps, Current: s.q.Printer(r.Context()), Telegram: s.tg != nil, Links: s.tg != nil && !s.tg.CallbacksWork()}
	s.render(w, r, "settings", page{Title: "Settings", Nav: "settings", V: v, Data: d})
}

func (s *Server) postSettings(w http.ResponseWriter, r *http.Request, v viewer) {
	atoi := func(k string, def, lo, hi int) int {
		n, err := strconv.Atoi(r.FormValue(k))
		if err != nil {
			return def
		}
		return min(max(n, lo), hi)
	}
	s.st.UpdateSettings(func(x *store.Settings) {
		x.Printer = r.FormValue("printer")
		x.GuestsEnabled = r.FormValue("guests") == "on"
		x.GuestPerHour = atoi("guestPerHour", x.GuestPerHour, 1, 50)
		x.GuestMaxPages = atoi("guestMaxPages", x.GuestMaxPages, 1, 500)
		x.RequestTTLHours = atoi("requestTTLHours", x.RequestTTLHours, 1, 168)
		x.FlipReverse = r.FormValue("flipReverse") == "on"
		x.AutoDuplex = r.FormValue("autoDuplex") == "on"
		x.NotifyTelegram = r.FormValue("notifyTelegram") == "on"
		x.NotifyIncoming = r.FormValue("notifyIncoming") == "on"
	})
	back(w, r, "/admin/settings", "Settings saved.")
}

func (s *Server) postTestAlert(w http.ResponseWriter, r *http.Request, v viewer) {
	if s.tg == nil {
		back(w, r, "/admin/settings", "Telegram isn't configured. Add TELEGRAM_BOT_TOKEN and TELEGRAM_CHAT_ID to printty.env.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if err := s.tg.Test(ctx, v.User.Name); err != nil {
		back(w, r, "/admin/settings", "Telegram said: "+err.Error())
		return
	}
	back(w, r, "/admin/settings", "Test message sent. Check Telegram.")
}
