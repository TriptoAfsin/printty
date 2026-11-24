//go:build windows

package printer

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestPrintToPDF prints through the real GDI path to "Microsoft Print to PDF",
// writing a file instead of using paper. Run with PRINTTY_PRINT_TEST=1.
func TestPrintToPDF(t *testing.T) {
	if os.Getenv("PRINTTY_PRINT_TEST") == "" {
		t.Skip("set PRINTTY_PRINT_TEST=1 to run")
	}
	b := New().(*Windows)
	ps, err := b.Printers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range ps {
		t.Logf("printer %q default=%v caps=%+v", p.Name, p.Default, p.Caps)
	}
	st, err := b.Status(context.Background(), "Canon G1010 series")
	t.Logf("G1010 status: %+v err=%v", st, err)

	src := writeTestPDF(t, 3)
	out := filepath.Join(t.TempDir(), "out.pdf")
	if d := os.Getenv("PRINTTY_RENDER_OUT"); d != "" {
		out = filepath.Join(d, "printed.pdf")
		os.Remove(out)
	}
	b.OutputFile = out
	job := Job{ID: "test1", Title: "test.pdf", Path: src, Kind: "pdf", Printer: "Microsoft Print to PDF",
		Options: Options{Copies: 1, Pages: "1,3", Color: true, Paper: "A4"}}
	if err := b.Print(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	// The spooler writes the file asynchronously.
	for i := 0; i < 100; i++ {
		if s, _ := b.Spool(context.Background(), job.Printer, job.ID); s == SpoolGone {
			break
		}
		sleepMs(100)
	}
	n, err := b.PageCount(context.Background(), out, "pdf")
	if err != nil {
		t.Fatalf("reading printed PDF: %v", err)
	}
	if n != 2 {
		t.Errorf("printed %d pages, want 2", n)
	}
}
