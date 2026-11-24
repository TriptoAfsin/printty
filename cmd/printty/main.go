// Command printty runs the Printty print server.
//
//	printty                      run in this window
//	printty -background          run without a window (used for start at sign-in)
//	printty service install      install as a Windows service (asks for admin)
//	printty service uninstall    remove the service
//	printty service start|stop|restart|status
//	printty tray                 the notification-area icon for the service
//	printty open                 open Printty in the browser (Start menu shortcut)
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/TriptoAfsin/printty/internal/config"
	"github.com/TriptoAfsin/printty/internal/memtrim"
	"github.com/TriptoAfsin/printty/internal/netinfo"
	"github.com/TriptoAfsin/printty/internal/notify"
	"github.com/TriptoAfsin/printty/internal/printer"
	"github.com/TriptoAfsin/printty/internal/queue"
	"github.com/TriptoAfsin/printty/internal/server"
	"github.com/TriptoAfsin/printty/internal/service"
	"github.com/TriptoAfsin/printty/internal/store"
	"github.com/TriptoAfsin/printty/internal/worker"
)

var version = "dev"

func main() {
	// "printty worker" is the short-lived child that renders and prints one job.
	if len(os.Args) > 1 && os.Args[1] == "worker" {
		os.Exit(worker.Serve(os.Stdin, os.Stdout))
	}
	if len(os.Args) > 1 && os.Args[1] == "service" {
		os.Exit(serviceCmd(os.Args[2:]))
	}
	if len(os.Args) > 1 && os.Args[1] == "tray" {
		os.Exit(trayCmd(os.Args[2:]))
	}
	if len(os.Args) > 1 && os.Args[1] == "open" {
		os.Exit(openCmd(os.Args[2:]))
	}

	fs := flag.NewFlagSet("printty", flag.ExitOnError)
	addr := fs.String("addr", "", "listen address (default :8631, or PRINTTY_ADDR)")
	data := fs.String("data", "", "data directory (or PRINTTY_DATA_DIR)")
	background := fs.Bool("background", false, "run without a console window, logging to the data folder")
	showVersion := fs.Bool("version", false, "print the version and exit")
	fs.Parse(os.Args[1:])
	if *showVersion {
		fmt.Println("printty", version)
		return
	}

	mode := "console"
	switch {
	case service.IsService():
		mode = "service"
	case *background:
		mode = "background"
	}
	if mode != "console" {
		// Services start in System32 and sign-in starts anywhere: read printty.env next to the exe.
		os.Chdir(service.ExeDir())
	}
	cfg := loadConfig(*addr, *data)

	// Keep the heap small: Printty idles for days between jobs.
	debug.SetGCPercent(50)
	debug.SetMemoryLimit(24 << 20)

	var out io.Writer = os.Stderr
	if mode != "console" {
		if mode == "background" {
			service.HideConsole()
		}
		if f, err := openLog(cfg.DataDir); err == nil {
			out = f
			defer f.Close()
		}
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(out, &slog.HandlerOptions{Level: slog.LevelInfo})))

	if mode == "service" {
		err := service.Run(func(ctx context.Context) error { return run(ctx, cfg, mode) })
		if err != nil {
			slog.Error("service stopped", "err", err)
			os.Exit(1)
		}
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, cfg, mode); err != nil {
		slog.Error("printty stopped", "err", err)
		os.Exit(1)
	}
}

func loadConfig(addr, data string) config.Config {
	cfg := config.Load()
	if addr != "" {
		cfg.Addr = addr
	}
	if data != "" {
		cfg.DataDir = data
	}
	if abs, err := filepath.Abs(cfg.DataDir); err == nil {
		cfg.DataDir = abs
	}
	return cfg
}

// openLog opens <data>/printty.log, starting fresh once it passes 5 MB.
func openLog(dir string) (*os.File, error) {
	os.MkdirAll(dir, 0o700)
	p := filepath.Join(dir, "printty.log")
	flags := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	if fi, err := os.Stat(p); err == nil && fi.Size() > 5<<20 {
		flags = os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	}
	return os.OpenFile(p, flags, 0o600)
}

func run(ctx context.Context, cfg config.Config, mode string) error {
	st, err := store.Open(cfg.DataDir)
	if err != nil {
		return fmt.Errorf("open data dir %s: %w", cfg.DataDir, err)
	}
	be, err := worker.New(printer.NewLight())
	if err != nil {
		return err
	}
	tg := notify.NewTelegram(cfg.TelegramBot, cfg.TelegramIDs)
	secure := cfg.TLSCert != "" && cfg.TLSKey != ""
	addrs := netinfo.URLs(cfg.Addr, secure)
	public := cfg.PublicURL
	if public == "" {
		public = netinfo.Primary(addrs) // links in Telegram alerts
	}
	q := queue.New(st, be, tg, public)
	srv, err := server.New(st, q, be, tg, cfg.MaxUploadMB, secure, cfg.Addr)
	if err != nil {
		return err
	}
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	srv.SetRuntime(server.Runtime{Mode: mode, DataDir: cfg.DataDir, Addr: cfg.Addr, Stop: stop})

	hs := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       10 * time.Minute, // big uploads on slow Wi-Fi
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}
	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return fmt.Errorf("can't listen on %s (is Printty already running?): %w", cfg.Addr, err)
	}
	errc := make(chan error, 1)
	go func() {
		if secure {
			errc <- hs.ServeTLS(ln, cfg.TLSCert, cfg.TLSKey)
		} else {
			errc <- hs.Serve(ln)
		}
	}()
	go q.Run(ctx)
	if mode != "service" { // a service has no desktop; "printty tray" shows its icon
		startTray(ctx, cfg, mode, stop)
	}

	local := localURL(cfg)
	slog.Info("printty is running", "version", version, "mode", mode, "url", local, "data", cfg.DataDir, "printer", q.Printer(ctx), "telegram", tg != nil)
	for _, a := range addrs {
		slog.Info("open on phones", "url", a.URL, "via", a.Label)
	}
	if mode == "console" && len(addrs) > 0 {
		fmt.Println("\n  Open Printty on phones and other computers on this network:")
		for _, a := range addrs {
			fmt.Printf("    %-40s %s\n", a.URL, a.Label)
		}
		fmt.Println("  If they can't connect, allow printty.exe in Windows Firewall for Private networks.")
	}
	if code := srv.SetupCode(); code != "" {
		codeFile := filepath.Join(cfg.DataDir, "setup-code.txt")
		os.WriteFile(codeFile, []byte(code+"\n"), 0o600)
		fmt.Printf("\n  First run: open %s/setup and enter setup code %s\n  (also saved in %s)\n\n", local, code, codeFile)
		slog.Info("first run: setup code saved", "file", codeFile)
	}
	if !secure {
		slog.Info("serving plain HTTP: phones can print, but installing the app and the share sheet need HTTPS (see README)")
	}

	// Settle, then hand startup memory back to the OS.
	go func() {
		select {
		case <-time.After(15 * time.Second):
			memtrim.Now()
		case <-ctx.Done():
		}
	}()

	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
	}
	sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return hs.Shutdown(sctx)
}

// serviceCmd handles "printty service <install|uninstall|start|stop|status>".
func serviceCmd(args []string) int {
	if !service.Supported {
		fmt.Fprintln(os.Stderr, "Windows services aren't available on this OS. Use systemd or launchd (see README).")
		return 2
	}
	if len(args) == 0 {
		fmt.Println("usage: printty service install|uninstall|start|stop|restart|status [-addr :8631] [-data DIR] [-start]")
		return 2
	}
	cmd := args[0]
	fs := flag.NewFlagSet("service", flag.ExitOnError)
	addr := fs.String("addr", "", "listen address for the service")
	data := fs.String("data", "", "data directory for the service")
	start := fs.Bool("start", false, "start the service after installing")
	result := fs.String("result", "", "write \"ok\" or the error to this file (used by the web UI)")
	fs.Parse(args[1:])

	report := func(err error) int {
		if *result != "" {
			msg := "ok"
			if err != nil {
				msg = err.Error()
			}
			os.WriteFile(*result, []byte(msg), 0o600)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "printty service "+cmd+":", err)
			return 1
		}
		return 0
	}

	switch cmd {
	case "status":
		s := service.Status()
		fmt.Printf("service installed: %v\nservice running:   %v\nstart at sign-in:  %v\n", s.Installed, s.Running, s.AtSignIn)
		return 0
	case "start", "stop", "restart":
		// Signed-in users may start and stop an installed service without a prompt.
		var err error
		switch cmd {
		case "start":
			err = service.Start()
		case "stop":
			err = service.Stop()
		default:
			err = service.Restart()
		}
		if !service.IsAccessDenied(err) {
			return report(err)
		}
	case "install", "uninstall":
	default:
		fmt.Fprintln(os.Stderr, "unknown command:", cmd)
		return 2
	}

	exe, err := os.Executable()
	if err != nil {
		return report(err)
	}
	if !service.IsAdmin() {
		// Ask Windows for admin rights and run this same command elevated.
		if err := service.Elevate(exe, append([]string{"service"}, args...)); err != nil {
			return report(err)
		}
		fmt.Println("Windows is asking for permission on this PC. Approve it to continue.")
		return 0
	}

	switch cmd {
	case "install":
		cfg := loadConfig(*addr, *data)
		svcArgs := []string{"-addr", cfg.Addr, "-data", cfg.DataDir}
		_, port, _ := net.SplitHostPort(cfg.Addr)
		if err := service.Install(exe, svcArgs, port); err != nil {
			return report(err)
		}
		report(nil)
		fmt.Println("Installed the Printty service. It starts automatically with Windows.")
		if *start && service.Status().Running {
			// Reinstalled over a running service: restart it on the new settings.
			if err := service.Restart(); err != nil {
				fmt.Fprintln(os.Stderr, "installed, but couldn't restart it:", err)
				return 1
			}
			fmt.Println("Restarted.")
			return 0
		}
		if *start {
			// The copy of Printty that asked for this may still hold the port: wait for it.
			for i := 0; i < 120; i++ {
				if ln, err := net.Listen("tcp", cfg.Addr); err == nil {
					ln.Close()
					break
				}
				time.Sleep(500 * time.Millisecond)
			}
			if err := service.Start(); err != nil {
				fmt.Fprintln(os.Stderr, "installed, but couldn't start it:", err)
				return 1
			}
			waitListening(cfg.Addr, 15*time.Second)
			fmt.Println("Started.")
		}
		return 0
	case "uninstall":
		return report(service.Uninstall())
	case "start":
		return report(service.Start())
	case "stop":
		return report(service.Stop())
	case "restart":
		return report(service.Restart())
	}
	return 0
}
