package server

import (
	"encoding/json"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"time"
)

type aboutData struct {
	Version, Mode, Exe, DataDir, GoVersion, Platform, Printer, Uptime string
	Started                                                           time.Time
	Telegram                                                          bool
	Links                                                             []string
}

// getAbout shows the version and how this copy of Printty is running.
func (s *Server) getAbout(w http.ResponseWriter, r *http.Request, v viewer) {
	exe, _ := os.Executable()
	mode := map[string]string{"service": "Windows service", "background": "In the background (started at sign-in)", "console": "In a window"}[s.rt.Mode]
	if mode == "" {
		mode = s.rt.Mode
	}
	d := aboutData{
		Version: s.rt.Version, Mode: mode, Exe: exe, DataDir: s.rt.DataDir,
		GoVersion: runtime.Version(), Platform: runtime.GOOS + "/" + runtime.GOARCH,
		Printer: s.q.Printer(r.Context()), Telegram: s.tg != nil, Started: s.rt.Started,
	}
	if !s.rt.Started.IsZero() {
		d.Uptime = roughDuration(time.Since(s.rt.Started))
	}
	for _, a := range s.Addresses() {
		d.Links = append(d.Links, a.URL)
	}
	s.render(w, r, "about", page{Title: "About Printty", Nav: "settings", V: v, Data: d})
}

func roughDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "less than a minute"
	case d < time.Hour:
		return plural(int(d.Minutes()), "minute")
	case d < 48*time.Hour:
		return plural(int(d.Hours()), "hour")
	default:
		return plural(int(d.Hours()/24), "day")
	}
}

func plural(n int, w string) string {
	if n == 1 {
		return "1 " + w
	}
	return strconv.Itoa(n) + " " + w + "s"
}

// apiLocalAlerts feeds the tray icon of a Windows service ("printty tray"),
// which shows new requests as Windows notifications. Only answers requests
// from this PC (by the TCP peer address); others get 404.
func (s *Server) apiLocalAlerts(w http.ResponseWriter, r *http.Request) {
	if !fromThisPC(r) {
		http.NotFound(w, r)
		return
	}
	since, _ := strconv.ParseUint(r.URL.Query().Get("since"), 10, 64)
	seq, alerts := s.q.DesktopAlerts(since)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(map[string]any{"seq": seq, "alerts": alerts, "version": s.rt.Version})
}
