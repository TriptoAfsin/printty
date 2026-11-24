package server

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/TriptoAfsin/printty/internal/service"
)

// Runtime is how this copy of Printty was started.
type Runtime struct {
	Mode    string // console, background, service
	DataDir string
	Addr    string
	Stop    func() // shuts this process down gracefully
	Version string
	Started time.Time
}

func (s *Server) SetRuntime(rt Runtime) { s.rt = rt }

type startupData struct {
	Supported bool
	Mode      string
	State     service.State
}

func (s *Server) startup() startupData {
	return startupData{Supported: service.Supported, Mode: s.rt.Mode, State: service.Status()}
}

// postAutostart turns "start when I sign in" on or off for the signed-in Windows user.
func (s *Server) postAutostart(w http.ResponseWriter, r *http.Request, v viewer) {
	if s.rt.Mode == "service" {
		back(w, r, "/admin/settings", "Printty runs as a Windows service, so it already starts with Windows.")
		return
	}
	if r.FormValue("on") == "1" {
		exe, err := os.Executable()
		if err != nil {
			back(w, r, "/admin/settings", "Couldn't find printty.exe.")
			return
		}
		cmd := service.Quote([]string{exe, "-background", "-addr", s.rt.Addr, "-data", s.rt.DataDir})
		if err := service.EnableAutostart(cmd); err != nil {
			back(w, r, "/admin/settings", "Couldn't turn it on: "+err.Error())
			return
		}
		back(w, r, "/admin/settings", "Printty will start when you sign in to Windows.")
		return
	}
	service.DisableAutostart()
	back(w, r, "/admin/settings", "Printty won't start at sign-in any more.")
}

// postServiceInstall asks Windows (with a permission prompt on this PC) to
// install Printty as a service, then hands over: this copy exits and the
// service takes the same address.
func (s *Server) postServiceInstall(w http.ResponseWriter, r *http.Request, v viewer) {
	if s.rt.Mode == "service" {
		back(w, r, "/admin/settings", "Printty is already running as a service.")
		return
	}
	exe, err := os.Executable()
	if err != nil {
		back(w, r, "/admin/settings", "Couldn't find printty.exe.")
		return
	}
	result := filepath.Join(s.rt.DataDir, "service-install.result")
	os.Remove(result)
	args := []string{"service", "install", "-start", "-addr", s.rt.Addr, "-data", s.rt.DataDir, "-result", result}
	if err := service.Elevate(exe, args); err != nil {
		back(w, r, "/admin/settings", "Not installed: "+err.Error()+".")
		return
	}
	// Wait for the elevated installer to report back (the person may take a moment to approve).
	var msg string
	for i := 0; i < 120; i++ {
		if b, err := os.ReadFile(result); err == nil {
			msg = strings.TrimSpace(string(b))
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	os.Remove(result)
	if msg != "ok" {
		if msg == "" {
			msg = "no answer from the installer"
		}
		back(w, r, "/admin/settings", "Not installed: "+msg+".")
		return
	}
	// Installed: stop this copy so the service can take over the port.
	service.DisableAutostart()
	// The service can't show an icon on the desktop: start its tray helper now
	// (Windows starts it at every sign-in from now on).
	if exe, err := os.Executable(); err == nil {
		tray := exec.Command(exe, "tray", "-addr", s.rt.Addr, "-welcome")
		tray.SysProcAttr = detached()
		if err := tray.Start(); err == nil {
			tray.Process.Release()
		}
	}
	s.render(w, r, "switching", page{Title: "Switching to the service", V: s.who(r)})
	go func() {
		time.Sleep(time.Second)
		s.rt.Stop()
	}()
}

// postServiceRemove uninstalls the service from inside it (it has admin rights)
// and stops. Printty then runs only when started by hand.
func (s *Server) postServiceRemove(w http.ResponseWriter, r *http.Request, v viewer) {
	if s.rt.Mode != "service" {
		if err := service.Elevate(mustExe(), []string{"service", "uninstall"}); err != nil {
			back(w, r, "/admin/settings", "Not removed: "+err.Error()+".")
			return
		}
		back(w, r, "/admin/settings", "Windows asked for permission to remove the service.")
		return
	}
	if err := service.RemoveSelf(); err != nil {
		back(w, r, "/admin/settings", "Not removed: "+err.Error())
		return
	}
	s.render(w, r, "switching", page{Title: "Service removed", V: s.who(r), Data: "removed"})
	go func() {
		time.Sleep(time.Second)
		s.rt.Stop()
	}()
}

func mustExe() string {
	exe, _ := os.Executable()
	return exe
}

// postSelfTest renders a test page in a worker, without printing, to check
// the PDF engine works in this setup (for example inside a Windows service).
func (s *Server) postSelfTest(w http.ResponseWriter, r *http.Request, v viewer) {
	t, ok := s.be.(interface {
		SelfTest(ctx context.Context) (string, error)
	})
	if !ok {
		back(w, r, "/admin/settings", "Self-test isn't available.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	msg, err := t.SelfTest(ctx)
	if err != nil {
		back(w, r, "/admin/settings", "Printing check failed ("+s.rt.Mode+"): "+err.Error())
		return
	}
	back(w, r, "/admin/settings", msg+" Running as: "+s.rt.Mode+".")
}
