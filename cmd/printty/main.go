// Command printty runs the Printty print server.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/TriptoAfsin/printty/internal/config"
	"github.com/TriptoAfsin/printty/internal/memtrim"
	"github.com/TriptoAfsin/printty/internal/notify"
	"github.com/TriptoAfsin/printty/internal/printer"
	"github.com/TriptoAfsin/printty/internal/queue"
	"github.com/TriptoAfsin/printty/internal/server"
	"github.com/TriptoAfsin/printty/internal/store"
	"github.com/TriptoAfsin/printty/internal/worker"
)

var version = "dev"

func main() {
	// "printty worker" is the short-lived child that renders and prints one job.
	if len(os.Args) > 1 && os.Args[1] == "worker" {
		os.Exit(worker.Serve(os.Stdin, os.Stdout))
	}
	addr := flag.String("addr", "", "listen address (default :8631, or PRINTTY_ADDR)")
	data := flag.String("data", "", "data directory (or PRINTTY_DATA_DIR)")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println("printty", version)
		return
	}

	// Keep the heap small: Printty idles for days between jobs.
	debug.SetGCPercent(50)
	debug.SetMemoryLimit(24 << 20)

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))
	cfg := config.Load()
	if *addr != "" {
		cfg.Addr = *addr
	}
	if *data != "" {
		cfg.DataDir = *data
	}
	if err := run(cfg); err != nil {
		slog.Error("printty stopped", "err", err)
		os.Exit(1)
	}
}

func run(cfg config.Config) error {
	st, err := store.Open(cfg.DataDir)
	if err != nil {
		return fmt.Errorf("open data dir %s: %w", cfg.DataDir, err)
	}
	be, err := worker.New(printer.NewLight())
	if err != nil {
		return err
	}
	tg := notify.NewTelegram(cfg.TelegramBot, cfg.TelegramIDs)
	q := queue.New(st, be, tg, cfg.PublicURL)
	secure := cfg.TLSCert != "" && cfg.TLSKey != ""
	srv, err := server.New(st, q, be, tg, cfg.MaxUploadMB, secure)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	hs := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       10 * time.Minute, // big uploads on slow Wi-Fi
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}
	errc := make(chan error, 1)
	go func() {
		if secure {
			errc <- hs.ListenAndServeTLS(cfg.TLSCert, cfg.TLSKey)
		} else {
			errc <- hs.ListenAndServe()
		}
	}()
	go q.Run(ctx)

	scheme := "http"
	if secure {
		scheme = "https"
	}
	host, port, _ := net.SplitHostPort(cfg.Addr)
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "localhost"
	}
	local := scheme + "://" + net.JoinHostPort(host, port)
	slog.Info("printty is running", "version", version, "url", local, "data", cfg.DataDir, "printer", q.Printer(ctx), "telegram", tg != nil)
	if code := srv.SetupCode(); code != "" {
		fmt.Printf("\n  First run: open %s/setup and enter setup code %s\n\n", local, code)
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
