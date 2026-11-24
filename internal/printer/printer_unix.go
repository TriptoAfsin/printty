//go:build !windows

package printer

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// CUPS is the Linux/macOS backend: `lp` and `lpstat`. CUPS renders PDFs itself.
type CUPS struct{}

// New returns the backend for this OS.
func New() Backend { return CUPS{} }

func run(ctx context.Context, name string, args ...string) (string, error) {
	var out, errb bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return out.String(), fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(errb.String()))
	}
	return out.String(), nil
}

func (CUPS) Printers(ctx context.Context) ([]Info, error) {
	out, err := run(ctx, "lpstat", "-e")
	if err != nil {
		return nil, err
	}
	def := ""
	if d, err := run(ctx, "lpstat", "-d"); err == nil {
		if _, after, ok := strings.Cut(d, ":"); ok {
			def = strings.TrimSpace(after)
		}
	}
	var ps []Info
	for _, name := range strings.Fields(out) {
		ps = append(ps, Info{Name: name, Default: name == def, Caps: Caps{Color: true, Papers: []string{"A4", "Letter", "4x6", "5x7", "A5", "Legal"}, DefaultPaper: "A4",
			Options: driverOptions(ctx, name)}})
	}
	return ps, nil
}

func (CUPS) Status(ctx context.Context, name string) (Status, error) {
	out, err := run(ctx, "lpstat", "-p", name)
	if err != nil {
		return Status{State: Unknown, Detail: "Printer not found"}, err
	}
	st := Status{State: Ready, OSSays: "Idle", Spooler: "Clear"}
	low := strings.ToLower(out)
	switch {
	case strings.Contains(low, "disabled"), strings.Contains(low, "offline"), strings.Contains(low, "not connected"):
		st.State, st.OSSays, st.Detail = Offline, "Stopped", "CUPS reports the printer offline or stopped."
	case strings.Contains(low, "printing"):
		st.OSSays = "Printing"
	}
	if q, err := run(ctx, "lpstat", "-o", name); err == nil {
		if n := len(strings.Split(strings.TrimSpace(q), "\n")); strings.TrimSpace(q) != "" {
			st.JobsInOS = n
			st.Spooler = fmt.Sprintf("%d waiting", n)
		}
	}
	return st, nil
}

var pdfPageRe = regexp.MustCompile(`/Type\s*/Page[^s]`)

func (CUPS) PageCount(ctx context.Context, path, kind string) (int, error) {
	if kind != "pdf" {
		return 1, nil
	}
	if out, err := run(ctx, "pdfinfo", path); err == nil {
		for _, line := range strings.Split(out, "\n") {
			if v, ok := strings.CutPrefix(line, "Pages:"); ok {
				return strconv.Atoi(strings.TrimSpace(v))
			}
		}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	n := len(pdfPageRe.FindAll(b, -1))
	if n == 0 {
		n = 1
	}
	return n, nil
}

func (CUPS) Print(ctx context.Context, job Job) error {
	args := []string{"-d", job.Printer, "-t", docName(job.ID) + " " + job.Title}
	copies := max(job.Options.Copies, 1)
	args = append(args, "-n", strconv.Itoa(copies))
	pages := job.Options.Pages
	if len(job.PageList) > 0 {
		var s []string
		for _, p := range job.PageList {
			if p > 0 { // CUPS can't leave a slot empty; number-up regroups
				s = append(s, strconv.Itoa(p))
			}
		}
		pages = strings.Join(s, ",")
	}
	if pages != "" {
		args = append(args, "-P", pages)
	}
	sides := "one-sided"
	switch job.Options.Duplex {
	case LongEdge:
		sides = "two-sided-long-edge"
	case ShortEdge:
		sides = "two-sided-short-edge"
	}
	if len(job.PageList) > 0 { // manual duplex pass
		sides = "one-sided"
	}
	mode := "monochrome"
	if job.Options.Color {
		mode = "color"
	}
	args = append(args, "-o", "sides="+sides, "-o", "print-color-mode="+mode)
	if m := cupsMedia(job.Options.Paper); m != "" {
		args = append(args, "-o", "media="+m)
	}
	args = append(args, layoutArgs(job.Options)...)
	args = append(args, job.Path)
	_, err := run(ctx, "lp", args...)
	return err
}

func cupsMedia(p string) string {
	switch p {
	case "A4":
		return "A4"
	case "Letter":
		return "Letter"
	case "Legal":
		return "Legal"
	case "A5":
		return "A5"
	case "4x6":
		return "4x6"
	case "5x7":
		return "5x7"
	}
	return ""
}

func (CUPS) Spool(ctx context.Context, printer, jobID string) (SpoolState, error) {
	out, err := run(ctx, "lpstat", "-W", "not-completed", "-o", printer, "-l")
	if err != nil {
		return SpoolWaiting, err
	}
	if !strings.Contains(out, docName(jobID)) {
		return SpoolGone, nil
	}
	return SpoolWaiting, nil
}

func docName(jobID string) string { return "Printty " + jobID }

// NewLight is the same as New: CUPS work already happens in other processes.
func NewLight() Backend { return CUPS{} }

func selfTest(path string) (string, error) {
	if _, err := exec.LookPath("lp"); err != nil {
		return "", fmt.Errorf("CUPS isn't installed (no lp command)")
	}
	return "CUPS is available; it renders PDFs itself.", nil
}
