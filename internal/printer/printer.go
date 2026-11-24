// Package printer talks to the operating system's printers. Each OS has its own
// Backend (printer_windows.go, printer_unix.go) behind the same interface.
package printer

import (
	"context"
	"errors"
)

// Duplex is how pages are placed on the sheet.
type Duplex string

const (
	Simplex   Duplex = "one-sided"
	LongEdge  Duplex = "two-sided"       // flip on the long edge (portrait books)
	ShortEdge Duplex = "two-sided-short" // flip on the short edge
)

// Options are the settings a person picks for one job.
type Options struct {
	Copies int    `json:"copies"`
	Pages  string `json:"pages"` // "" = all, else "1-3,5"
	Duplex Duplex `json:"duplex"`
	Color  bool   `json:"color"`
	Paper  string `json:"paper"` // "A4", "Letter", "4x6"
}

// Caps describes what a printer can do.
type Caps struct {
	Color        bool     `json:"color"`
	AutoDuplex   bool     `json:"autoDuplex"`   // a duplex unit; false means manual flip
	Papers       []string `json:"papers"`       // paper names the UI offers
	DefaultPaper string   `json:"defaultPaper"` // what Printty preselects
}

// Info is one installed printer.
type Info struct {
	Name    string `json:"name"`
	Default bool   `json:"default"`
	Caps    Caps   `json:"caps"`
}

// State is the printer's readiness, combined from several signals.
type State string

const (
	Ready   State = "ready"
	Offline State = "offline"
	Error   State = "error"
	Unknown State = "unknown"
)

// Status is what the status monitor reports for a printer.
type Status struct {
	State    State  `json:"state"`
	OSSays   string `json:"osSays"`   // the OS's own wording, e.g. "Idle"
	Spooler  string `json:"spooler"`  // "Clear" or "2 jobs, 1 stuck"
	Detail   string `json:"detail"`   // human sentence for the UI
	JobsInOS int    `json:"jobsInOS"` // jobs sitting in the OS queue
}

// Job is what the queue hands the backend.
type Job struct {
	ID       string
	Title    string // document name shown in the OS spooler
	Path     string // file on disk
	Kind     string // "pdf", "png", "jpeg"
	Printer  string
	Options  Options
	PageList []int // explicit 1-based pages to print, in order (used for manual duplex)
}

// SpoolState is what happened to a job after it left Printty.
type SpoolState string

const (
	SpoolGone    SpoolState = "gone"    // the OS finished it (printed or deleted)
	SpoolWaiting SpoolState = "waiting" // still in the OS queue, progressing
	SpoolStuck   SpoolState = "stuck"   // in the OS queue with an error/offline flag or no progress
)

// Backend is one OS's printing implementation.
type Backend interface {
	Printers(ctx context.Context) ([]Info, error)
	Status(ctx context.Context, printer string) (Status, error)
	PageCount(ctx context.Context, path, kind string) (int, error)
	// Print spools the job and returns once the OS has it.
	Print(ctx context.Context, job Job) error
	// Spool reports where a previously printed job is in the OS queue.
	Spool(ctx context.Context, printer, jobID string) (SpoolState, error)
}

// ErrUnsupported is returned for file kinds a backend can't print.
var ErrUnsupported = errors.New("printer: unsupported file type")

// PaperNames are the sizes Printty knows how to ask for, in UI order.
var PaperNames = []string{"A4", "Letter", "4x6", "5x7", "A5", "Legal"}
