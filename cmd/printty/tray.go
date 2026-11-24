package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/TriptoAfsin/printty/internal/config"
	"github.com/TriptoAfsin/printty/internal/memtrim"
	"github.com/TriptoAfsin/printty/internal/netinfo"
	"github.com/TriptoAfsin/printty/internal/queue"
	"github.com/TriptoAfsin/printty/internal/service"
	"github.com/TriptoAfsin/printty/internal/tray"
	"github.com/TriptoAfsin/printty/internal/web"
)

func trayIcon() []byte {
	b, _ := web.Static.ReadFile("static/icon-192.png")
	return b
}

// localURL is the address to open Printty on this PC.
func localURL(cfg config.Config) string {
	scheme := "http"
	if cfg.TLSCert != "" && cfg.TLSKey != "" {
		scheme = "https"
	}
	host, port, _ := net.SplitHostPort(cfg.Addr)
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "localhost"
	}
	return scheme + "://" + net.JoinHostPort(host, port)
}

// phoneLink is the address other devices on the network should open.
func phoneLink(cfg config.Config) string {
	return netinfo.Primary(netinfo.URLs(cfg.Addr, cfg.TLSCert != "" && cfg.TLSKey != ""))
}

func copyItem(cfg config.Config) tray.Item {
	link := phoneLink(cfg)
	if link == "" {
		return tray.Item{Label: "No network for phones", Disabled: true}
	}
	return tray.Item{Label: "Copy link for phones  (" + link + ")", Do: func() { tray.CopyText(link) }}
}

// startTray shows the icon for a copy of Printty started by hand or at sign-in.
// Exit stops this process.
func startTray(ctx context.Context, cfg config.Config, mode string, stop func(), alerts alertSource) {
	if !tray.Supported {
		return
	}
	local := localURL(cfg)
	how := "Running in this window"
	if mode == "background" {
		how = "Running in the background"
	}
	t, err := tray.Start(ctx, tray.Config{
		Icon:          trayIcon(),
		State:         func() (string, bool) { return "Printty " + version + ": running\n" + local, true },
		OnNotifyClick: func() { tray.Open(local + "/admin") },
		Menu: func() []tray.Item {
			items := []tray.Item{
				{Label: "Printty " + version + " · " + how, Disabled: true},
				{Label: "Open Printty", Default: true, Do: func() { tray.Open(local + "/admin") }},
				copyItem(cfg),
				{Label: "About Printty", Do: func() { tray.Open(local + "/admin/about") }},
			}
			if mode == "background" {
				items = append(items, tray.Item{Label: "Show log", Do: func() { tray.Open(filepath.Join(cfg.DataDir, "printty.log")) }})
			}
			return append(items, tray.Item{}, tray.Item{Label: "Exit Printty", Do: stop})
		},
	})
	if err != nil {
		slog.Warn("no tray icon", "err", err)
		return
	}
	go showAlerts(ctx, t, 3*time.Second, func(since uint64) (uint64, []queue.DesktopAlert, error) {
		seq, a := alerts.DesktopAlerts(since)
		return seq, a, nil
	})
}

type alertSource interface {
	DesktopAlerts(since uint64) (uint64, []queue.DesktopAlert)
}

// showAlerts turns new desktop alerts into Windows notifications. The first
// fetch only finds where to start, so old alerts aren't replayed.
func showAlerts(ctx context.Context, t *tray.Tray, every time.Duration, fetch func(since uint64) (uint64, []queue.DesktopAlert, error)) {
	var since uint64
	started := false
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		seq, alerts, err := fetch(since)
		if err == nil {
			if seq < since { // Printty restarted: its numbering starts again
				since, started = 0, false
				continue
			}
			if started && len(alerts) == 1 {
				t.Notify(alerts[0].Title, alerts[0].Text)
			} else if started && len(alerts) > 1 {
				last := alerts[len(alerts)-1]
				t.Notify(fmt.Sprintf("%d new alerts", len(alerts)), last.Title+": "+last.Text)
			}
			since, started = seq, true
		}
		select {
		case <-ctx.Done():
			return
		case <-t.Done():
			return
		case <-tick.C:
		}
	}
}

// trayCmd is "printty tray": the icon for the Windows service, started at
// every sign-in (a service runs in its own session and can't show one).
// It controls the service through Windows, not through the web server.
func trayCmd(args []string) int {
	fs := flag.NewFlagSet("tray", flag.ExitOnError)
	addr := fs.String("addr", "", "the service's listen address")
	welcome := fs.Bool("welcome", false, "say hello once (after installing)")
	fs.Parse(args)
	if !tray.Supported {
		fmt.Fprintln(os.Stderr, "the tray icon is only available on Windows")
		return 2
	}
	service.HideConsole()
	if !tray.Single("Printty tray") {
		return 0
	}
	debug.SetGCPercent(50)
	debug.SetMemoryLimit(16 << 20)
	os.Chdir(service.ExeDir())
	cfg := loadConfig(*addr, "")
	local := localURL(cfg)
	exe, _ := os.Executable()

	ctx, quit := context.WithCancel(context.Background())
	defer quit()
	var t *tray.Tray
	control := func(op string) {
		var err error
		switch op {
		case "start":
			err = service.Start()
		case "stop":
			err = service.Stop()
		case "restart":
			err = service.Restart()
		}
		if service.IsAccessDenied(err) {
			err = service.Elevate(exe, []string{"service", op})
		}
		if err != nil {
			t.Notify("Printty", "Couldn't "+op+" Printty: "+err.Error())
		}
	}

	var err error
	t, err = tray.Start(ctx, tray.Config{
		Icon:          trayIcon(),
		Every:         5 * time.Second,
		OnNotifyClick: func() { tray.Open(local + "/admin") },
		State: func() (string, bool) {
			st := service.Status()
			if !st.Installed {
				quit() // the service was removed: nothing left to show
			}
			if st.Running {
				return "Printty: running\n" + local, true
			}
			return "Printty: stopped", false
		},
		Menu: func() []tray.Item {
			if !service.Status().Running {
				return []tray.Item{
					{Label: "Printty " + version + " is stopped", Disabled: true},
					{Label: "Start Printty", Default: true, Do: func() { control("start") }},
					{},
					{Label: "Hide this icon", Do: quit},
				}
			}
			return []tray.Item{
				{Label: "Printty " + version + " · Running as a Windows service", Disabled: true},
				{Label: "Open Printty", Default: true, Do: func() { tray.Open(local + "/admin") }},
				copyItem(cfg),
				{Label: "About Printty", Do: func() { tray.Open(local + "/admin/about") }},
				{},
				{Label: "Restart Printty", Do: func() { control("restart") }},
				{Label: "Stop Printty", Do: func() { control("stop") }},
				{Label: "Hide this icon", Do: quit},
			}
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "printty tray:", err)
		return 1
	}
	if *welcome {
		t.Notify("Printty is running", "It starts with Windows now. Click this icon to open it; right-click to copy the link for phones or stop it.")
	}
	go func() {
		time.Sleep(10 * time.Second)
		memtrim.Now()
	}()
	// New requests from the service, shown as Windows notifications.
	client := &http.Client{Timeout: 3 * time.Second}
	_, port, _ := net.SplitHostPort(cfg.Addr)
	api := "http://" + net.JoinHostPort("127.0.0.1", port) + "/api/local/alerts?since="
	go showAlerts(ctx, t, 5*time.Second, func(since uint64) (uint64, []queue.DesktopAlert, error) {
		resp, err := client.Get(api + strconv.FormatUint(since, 10))
		if err != nil {
			return 0, nil, err
		}
		defer resp.Body.Close()
		var body struct {
			Seq    uint64               `json:"seq"`
			Alerts []queue.DesktopAlert `json:"alerts"`
		}
		if resp.StatusCode != http.StatusOK {
			return 0, nil, fmt.Errorf("status %d", resp.StatusCode)
		}
		err = json.NewDecoder(resp.Body).Decode(&body)
		return body.Seq, body.Alerts, err
	})
	<-t.Done()
	return 0
}

// waitListening waits until something accepts connections on addr.
func waitListening(addr string, max time.Duration) bool {
	_, port, _ := net.SplitHostPort(addr)
	end := time.Now().Add(max)
	for {
		if c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", port), time.Second); err == nil {
			c.Close()
			return true
		}
		if time.Now().After(end) {
			return false
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// openCmd is "printty open", the Start menu shortcut: it starts the service if
// it's stopped, makes sure the tray icon is there, and opens the admin page.
func openCmd(args []string) int {
	fs := flag.NewFlagSet("open", flag.ExitOnError)
	addr := fs.String("addr", "", "listen address")
	fs.Parse(args)
	service.HideConsole()
	os.Chdir(service.ExeDir())
	cfg := loadConfig(*addr, "")
	st := service.Status()
	if st.Installed {
		if !st.Running {
			if err := service.Start(); service.IsAccessDenied(err) {
				exe, _ := os.Executable()
				service.Elevate(exe, []string{"service", "start"})
			}
			waitListening(cfg.Addr, 15*time.Second)
		}
		if exe, err := os.Executable(); err == nil {
			startDetached(exe, "tray", "-addr", cfg.Addr) // quits at once if it's already showing
		}
	} else if !waitListening(cfg.Addr, 0) {
		// Not installed and not running: run Printty here, in the background.
		if exe, err := os.Executable(); err == nil {
			startDetached(exe, "-background", "-addr", cfg.Addr)
			waitListening(cfg.Addr, 15*time.Second)
		}
	}
	if err := tray.Open(localURL(cfg) + "/admin"); err != nil {
		fmt.Fprintln(os.Stderr, "open the browser:", err)
		return 1
	}
	return 0
}
