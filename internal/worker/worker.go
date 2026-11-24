// Package worker runs the heavy printing work (PDF rendering, printer drivers)
// in a short-lived child process: the same printty binary, started with
// "printty worker". When the child exits, every DLL, thread and buffer it
// loaded goes with it, so the long-running server stays small.
package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/TriptoAfsin/printty/internal/printer"
)

// Request is what the parent sends on the child's stdin.
type Request struct {
	Op      string      `json:"op"` // print, pages, caps
	Job     printer.Job `json:"job,omitempty"`
	Path    string      `json:"path,omitempty"`
	Kind    string      `json:"kind,omitempty"`
	Printer string      `json:"printer,omitempty"`
}

// Response is what the child writes on stdout.
type Response struct {
	Error string       `json:"error,omitempty"`
	Pages int          `json:"pages,omitempty"`
	Caps  printer.Caps `json:"caps,omitempty"`
}

// Serve is the child side: one request in, one response out.
func Serve(in io.Reader, out io.Writer) int {
	var req Request
	if err := json.NewDecoder(in).Decode(&req); err != nil {
		json.NewEncoder(out).Encode(Response{Error: "bad request"})
		return 2
	}
	be := printer.New()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	var resp Response
	var err error
	switch req.Op {
	case "print":
		err = be.Print(ctx, req.Job)
	case "pages":
		resp.Pages, err = be.PageCount(ctx, req.Path, req.Kind)
	case "caps":
		var ps []printer.Info
		ps, err = be.Printers(ctx)
		for _, p := range ps {
			if p.Name == req.Printer {
				resp.Caps = p.Caps
			}
		}
	default:
		err = fmt.Errorf("unknown op %q", req.Op)
	}
	if err != nil {
		resp.Error = err.Error()
	}
	json.NewEncoder(out).Encode(resp)
	if err != nil {
		return 1
	}
	return 0
}

// Backend wraps the in-process backend: light calls (status, spooler, printer
// names) stay in-process; heavy ones run in a child process.
type Backend struct {
	Light printer.Backend // its Printers() must not load drivers (no caps)
	Exe   string          // path to the printty binary

	mu   sync.Mutex
	caps map[string]printer.Caps
}

func New(light printer.Backend) (*Backend, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	return &Backend{Light: light, Exe: exe, caps: map[string]printer.Caps{}}, nil
}

func (b *Backend) call(ctx context.Context, req Request) (Response, error) {
	in, _ := json.Marshal(req)
	cmd := exec.CommandContext(ctx, b.Exe, "worker")
	cmd.Stdin = bytes.NewReader(in)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	hideWindow(cmd)
	runErr := cmd.Run()
	var resp Response
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		msg := strings.TrimSpace(errb.String())
		if runErr != nil && msg == "" {
			msg = runErr.Error()
		}
		return resp, fmt.Errorf("print worker failed: %s", msg)
	}
	if resp.Error != "" {
		return resp, fmt.Errorf("%s", resp.Error)
	}
	return resp, nil
}

func (b *Backend) Printers(ctx context.Context) ([]printer.Info, error) {
	ps, err := b.Light.Printers(ctx)
	if err != nil {
		return nil, err
	}
	for i := range ps {
		ps[i].Caps = b.Caps(ctx, ps[i].Name)
	}
	return ps, nil
}

// Caps asks a child for a printer's capabilities once and caches them.
func (b *Backend) Caps(ctx context.Context, name string) printer.Caps {
	b.mu.Lock()
	c, ok := b.caps[name]
	b.mu.Unlock()
	if ok {
		return c
	}
	resp, err := b.call(ctx, Request{Op: "caps", Printer: name})
	c = resp.Caps
	if err != nil || len(c.Papers) == 0 {
		c = printer.Caps{Color: true, Papers: []string{"A4", "Letter"}, DefaultPaper: "A4"}
		if err != nil {
			return c // don't cache failures
		}
	}
	b.mu.Lock()
	b.caps[name] = c
	b.mu.Unlock()
	return c
}

func (b *Backend) Status(ctx context.Context, name string) (printer.Status, error) {
	return b.Light.Status(ctx, name)
}

func (b *Backend) Spool(ctx context.Context, name, jobID string) (printer.SpoolState, error) {
	return b.Light.Spool(ctx, name, jobID)
}

func (b *Backend) PageCount(ctx context.Context, path, kind string) (int, error) {
	if kind != "pdf" {
		return 1, nil
	}
	resp, err := b.call(ctx, Request{Op: "pages", Path: path, Kind: kind})
	return resp.Pages, err
}

func (b *Backend) Print(ctx context.Context, job printer.Job) error {
	_, err := b.call(ctx, Request{Op: "print", Job: job})
	return err
}
