// Package printer talks to the operating system's printers. Each OS has its own
// Backend (printer_windows.go, printer_unix.go) behind the same interface.
package printer

import (
	"context"
	"errors"
	"strings"
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

	// Layout, applied the same way on every OS (see layout.go).
	Orientation  Orientation `json:"orientation,omitempty"`  // "" = auto
	Fit          Fit         `json:"fit,omitempty"`          // "" = fit the page
	PerSheet     int         `json:"perSheet,omitempty"`     // pages per sheet: 1 (or 0), 2, 3, 4, 6, 8, 9
	NoAutoRotate bool        `json:"noAutoRotate,omitempty"` // don't turn pages to suit the sheet
	Rotate       map[int]int `json:"rotate,omitempty"`       // page -> clockwise degrees; overrides auto-rotate

	// Photo layout (offered for images, works for any page).
	Repeat int     `json:"repeat,omitempty"` // copies of each page side by side on one sheet: 2, 3, 4, 6, 8
	Scale  int     `json:"scale,omitempty"`  // percent of the size Fit/Fill/Actual gives: 10-100 (0 = 100)
	OffX   float64 `json:"offX,omitempty"`   // position in the free space: -1 left, 0 centre, 1 right
	OffY   float64 `json:"offY,omitempty"`   // -1 top, 0 centre, 1 bottom

	// OneSheet puts every chosen page on one sheet in the grid that shows
	// them biggest (photo sets). Place moves single pages on that sheet:
	// page -> centre as a fraction of the printable area, 0..1 each way.
	OneSheet bool               `json:"oneSheet,omitempty"`
	Place    map[int][2]float64 `json:"place,omitempty"`

	// Binding margin: blank space down one side of every sheet, measured
	// from the paper's edge, so the print can be stapled, punched or bound.
	// On the back of a two-sided sheet it swaps sides (see layout.go).
	BindSide string `json:"bindSide,omitempty"` // BindLeft, BindRight or "" for none
	BindMM   int    `json:"bindMM,omitempty"`   // BindMinMM..BindMaxMM

	// Set for one print run, never stored. BackFlip: this manual-duplex pass
	// prints only backs, flipped this way. BindEdge: the printer's own
	// unprintable edge in inches, which already counts towards BindMM.
	BackFlip Duplex  `json:"-"`
	BindEdge float64 `json:"-"`

	// Driver holds printer-specific choices by Caps.Options key (paper type,
	// paper source, quality...). Each backend maps them to its own system.
	Driver map[string]string `json:"driver,omitempty"`
}

// Orientation of the sheet.
type Orientation string

const (
	AutoOrientation Orientation = ""
	Portrait        Orientation = "portrait"
	Landscape       Orientation = "landscape"
)

// Fit is how a page is scaled into its space on the sheet.
type Fit string

const (
	FitPage Fit = ""       // whole page visible, as large as fits
	Fill    Fit = "fill"   // cover the space, trimming edges (photos)
	Actual  Fit = "actual" // 100%, centred, trimmed if larger
)

// Caps describes what a printer can do.
type Caps struct {
	Color        bool     `json:"color"`
	AutoDuplex   bool     `json:"autoDuplex"`   // a duplex unit; false means manual flip
	Papers       []string `json:"papers"`       // paper names the UI offers
	DefaultPaper string   `json:"defaultPaper"` // what Printty preselects
	// Options are the printer's own extra settings, read from its driver
	// (Windows) or its PPD options (CUPS). The UI shows them generically.
	Options []DriverOption `json:"options,omitempty"`
}

// DriverOption is one printer-specific setting, such as paper type.
type DriverOption struct {
	Key     string   `json:"key"`   // "media", "source", "quality", or a CUPS option name
	Label   string   `json:"label"` // shown in the UI
	Default string   `json:"default"`
	Choices []Choice `json:"choices"`
}

// Choice is one value of a DriverOption.
type Choice struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// Option returns the driver option for one of the settings the print page
// shows on its own: OptQuality or OptMedia. CUPS printers name them in
// their PPD (cupsPrintQuality, MediaType...), Windows uses the keys as is.
func (c Caps) Option(which string) *DriverOption {
	for i, o := range c.Options {
		k := strings.ToLower(o.Key)
		switch {
		case which == OptQuality && strings.Contains(k, "quality"),
			which == OptMedia && (k == "media" || k == "mediatype" || k == "media-type"):
			return &c.Options[i]
		}
	}
	return nil
}

// Settings the print page shows by themselves (Caps.Option).
const (
	OptQuality = "quality"
	OptMedia   = "media"
)

// Valid reports whether value is one of the option's choices.
func (o DriverOption) Valid(value string) bool {
	for _, c := range o.Choices {
		if c.Value == value {
			return true
		}
	}
	return false
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
	ID      string
	Title   string // document name shown in the OS spooler
	Path    string // file on disk
	Kind    string // "pdf", "png", "jpeg"
	Printer string
	Options Options
	// PageList is the explicit 1-based pages to print, in order (manual
	// duplex). 0 is an empty slot, so sheets keep their grouping.
	PageList []int
	// BackFlip: this manual-duplex pass prints only backs, flipped this way
	// (a binding margin swaps sides on them). Backends copy it into
	// Options.BackFlip; it's here because Options.BackFlip isn't sent to
	// the worker.
	BackFlip Duplex
	// SystemLayout: on CUPS, let the print system lay pages out (number-up,
	// fit) instead of printing the sheets Printty composes (ComposeLayout).
	SystemLayout bool
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

// Inspector is an optional Backend feature: count the pages and write small
// preview images (dir/1.png, dir/2.png...) for the first max pages.
type Inspector interface {
	Inspect(ctx context.Context, path, kind, dir string, max int) (pages int, err error)
}

// ErrUnsupported is returned for file kinds a backend can't print.
var ErrUnsupported = errors.New("printer: unsupported file type")

// PaperNames are the sizes Printty knows how to ask for, in UI order.
var PaperNames = []string{"A4", "Letter", "4x6", "5x7", "A5", "Legal"}
