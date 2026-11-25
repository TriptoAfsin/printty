package printer

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
)

// sheetsOf reads a composed PDF back: each page's size in points.
func sheetsOf(t *testing.T, path string) [][2]float64 {
	t.Helper()
	ctx, err := api.ReadContextFile(context.Background(), path)
	if err != nil {
		t.Fatalf("composed PDF doesn't read back: %v", err)
	}
	var out [][2]float64
	for p := 1; p <= ctx.PageCount; p++ {
		_, _, attrs, err := ctx.PageDict(context.Background(), p, false)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, [2]float64{math.Round(attrs.MediaBox.Width()), math.Round(attrs.MediaBox.Height())})
	}
	return out
}

func TestComposeLayout(t *testing.T) {
	src := writeTestPDF(t, 3)
	a4, a4l := [2]float64{595, 842}, [2]float64{842, 595}
	tests := []struct {
		name  string
		pages []int
		o     Options
		want  [][2]float64
	}{
		{"one per sheet", nil, Options{Paper: "A4"}, [][2]float64{a4, a4, a4}},
		{"two per sheet on a landscape sheet", nil, Options{Paper: "A4", PerSheet: 2}, [][2]float64{a4l, a4l}},
		{"page range", nil, Options{Paper: "A4", Pages: "3,1"}, [][2]float64{a4, a4}},
		{"letter paper", []int{1}, Options{Paper: "Letter"}, [][2]float64{{612, 792}}},
		{"manual pass keeps empty slots", []int{1, 0, 3, 0}, Options{Paper: "A4", PerSheet: 2}, [][2]float64{a4l, a4l}},
		{"binding margin, two-sided", nil, Options{Paper: "A4", BindSide: BindLeft, BindMM: 20, Duplex: LongEdge}, [][2]float64{a4, a4, a4}},
		{"all on one sheet, turned page", nil, Options{Paper: "A4", OneSheet: true, Rotate: map[int]int{2: 90}}, [][2]float64{a4l}},
	}
	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), fmt.Sprintf("sheets-%d.pdf", i))
			n, err := ComposeLayout(context.Background(), src, "pdf", tc.pages, tc.o, 0.17, out)
			if err != nil {
				t.Fatal(err)
			}
			got := sheetsOf(t, out)
			if n != len(got) || fmt.Sprint(got) != fmt.Sprint(tc.want) {
				t.Errorf("sheets = %d %v, want %v", n, got, tc.want)
			}
		})
	}
}

func TestComposeLayoutPhoto(t *testing.T) {
	dir := t.TempDir()
	photos := writePhotos(t, dir)
	out := filepath.Join(dir, "sheets.pdf")
	n, err := ComposeLayout(context.Background(), photos[0], "jpeg", nil, Options{Paper: "A4", Repeat: 4}, 0.17, out)
	if err != nil || n != 1 {
		t.Fatalf("n = %d, err = %v", n, err)
	}
	if got := sheetsOf(t, out); len(got) != 1 {
		t.Errorf("sheets = %v", got)
	}
}

func TestComposeLayoutBadFile(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "bad.pdf")
	if err := os.WriteFile(bad, []byte("%PDF-1.4\nnot really a pdf"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ComposeLayout(context.Background(), bad, "pdf", nil, Options{}, 0.17, bad+".out"); err == nil {
		t.Error("a broken PDF composed without an error; the CUPS fallback would never run")
	}
}

// placeMatrix must land the form's corners exactly on the target box.
func TestPlaceMatrix(t *testing.T) {
	const sw, sh = 200.0, 100.0 // a landscape source
	for _, rot := range []int{0, 90, 180, 270} {
		x, y, w, h := 50.0, 70.0, 300.0, 150.0
		if rot == 90 || rot == 270 {
			w, h = 150, 300
		}
		var a, b, c, d, e, f float64
		var cm string
		fmt.Sscanf(placeMatrix(rot, sw, sh, x, y, w, h), "%f %f %f %f %f %f %s", &a, &b, &c, &d, &e, &f, &cm)
		minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
		for _, p := range [][2]float64{{0, 0}, {sw, 0}, {0, sh}, {sw, sh}} {
			px, py := a*p[0]+c*p[1]+e, b*p[0]+d*p[1]+f
			minX, minY, maxX, maxY = math.Min(minX, px), math.Min(minY, py), math.Max(maxX, px), math.Max(maxY, py)
		}
		if !near(minX, x) || !near(minY, y) || !near(maxX, x+w) || !near(maxY, y+h) {
			t.Errorf("rot %d: box %.1f,%.1f-%.1f,%.1f, want %.1f,%.1f-%.1f,%.1f", rot, minX, minY, maxX, maxY, x, y, x+w, y+h)
		}
		// Clockwise as seen: the source's top edge ends up on the right for 90.
		if rot == 90 {
			topX := a*(sw/2) + c*sh + e
			if !near(topX, x+w) {
				t.Errorf("rot 90 turned the wrong way: top edge at x=%.1f", topX)
			}
		}
	}
	if !strings.HasSuffix(placeMatrix(0, 1, 1, 0, 0, 1, 1), " cm") {
		t.Error("no cm operator")
	}
}
