package printer

import (
	"math"
	"reflect"
	"testing"
)

// A4 printable area in inches (roughly what a G1010 reports).
const areaW, areaH = 8.0, 11.4

func sizes(m map[int][2]float64) func(int) (float64, float64) {
	return func(p int) (float64, float64) {
		if s, ok := m[p]; ok {
			return s[0], s[1]
		}
		return 8.27, 11.69 // A4 portrait
	}
}

func near(a, b float64) bool { return math.Abs(a-b) < 0.01 }

func TestLayoutAutoRotate(t *testing.T) {
	// Page 2 is landscape, like the receipt in the bug report.
	size := sizes(map[int][2]float64{2: {11.69, 8.27}})
	tests := []struct {
		name string
		o    Options
		want []int // rotation per page
	}{
		{"auto turns the landscape page", Options{}, []int{0, 90}},
		{"auto-rotate off keeps it", Options{NoAutoRotate: true}, []int{0, 0}},
		{"a manual turn wins", Options{Rotate: map[int]int{2: 270, 1: 180}}, []int{180, 270}},
		{"odd angles are normalised", Options{Rotate: map[int]int{1: -90}}, []int{270, 90}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sheets := Layout([]int{1, 2}, size, areaW, areaH, tc.o)
			if len(sheets) != 2 {
				t.Fatalf("sheets = %d", len(sheets))
			}
			for i, sh := range sheets {
				if sh[0].Rotate != tc.want[i] {
					t.Errorf("page %d rotate = %d, want %d", i+1, sh[0].Rotate, tc.want[i])
				}
			}
		})
	}
}

func TestLayoutFitModes(t *testing.T) {
	size := sizes(nil)
	fit := Layout([]int{1}, size, areaW, areaH, Options{})[0][0]
	if fit.Rect.W > areaW+0.001 || fit.Rect.H > areaH+0.001 {
		t.Errorf("fit overflows: %+v", fit.Rect)
	}
	if !near(fit.Rect.H, areaH) && !near(fit.Rect.W, areaW) {
		t.Errorf("fit doesn't touch an edge: %+v", fit.Rect)
	}
	fill := Layout([]int{1}, size, areaW, areaH, Options{Fit: Fill})[0][0]
	if fill.Rect.W < areaW-0.001 || fill.Rect.H < areaH-0.001 {
		t.Errorf("fill leaves gaps: %+v", fill.Rect)
	}
	actual := Layout([]int{1}, size, areaW, areaH, Options{Fit: Actual})[0][0]
	if !near(actual.Rect.W, 8.27) || !near(actual.Rect.H, 11.69) {
		t.Errorf("actual size changed: %+v", actual.Rect)
	}
	// Centred in every mode.
	for _, p := range []Placement{fit, fill, actual} {
		if !near(p.Rect.X+p.Rect.W/2, areaW/2) || !near(p.Rect.Y+p.Rect.H/2, areaH/2) {
			t.Errorf("not centred: %+v", p.Rect)
		}
	}
}

func TestLayoutPerSheet(t *testing.T) {
	size := sizes(nil)
	// 2 per sheet on a landscape sheet: side by side, both portrait.
	o := Options{PerSheet: 2}
	if got := SheetOrientation(o, 8.27, 11.69); got != Landscape {
		t.Fatalf("2-up orientation = %q", got)
	}
	sheets := Layout([]int{1, 2, 3}, size, areaH, areaW, o)
	if len(sheets) != 2 || len(sheets[0]) != 2 || len(sheets[1]) != 1 {
		t.Fatalf("grouping = %v", sheets)
	}
	a, b := sheets[0][0], sheets[0][1]
	if a.Rotate != 0 || b.Rotate != 0 {
		t.Errorf("portrait pages turned on 2-up: %d %d", a.Rotate, b.Rotate)
	}
	if !(a.Cell.X < b.Cell.X) || !near(a.Cell.Y, b.Cell.Y) {
		t.Errorf("not side by side: %+v %+v", a.Cell, b.Cell)
	}
	if a.Rect.X+a.Rect.W > b.Rect.X+0.001 {
		t.Errorf("pages overlap")
	}
	// 4 per sheet on portrait: 2x2.
	four := Layout([]int{1, 2, 3, 4}, size, areaW, areaH, Options{PerSheet: 4})[0]
	if len(four) != 4 || !near(four[0].Cell.Y, four[1].Cell.Y) || !(four[2].Cell.Y > four[0].Cell.Y) {
		t.Errorf("4-up grid wrong: %+v", four)
	}
	// Empty slots (0) leave a gap but keep the grouping.
	gap := Layout([]int{1, 0, 3, 4}, size, areaH, areaW, o)
	if len(gap) != 2 || len(gap[0]) != 1 || len(gap[1]) != 2 {
		t.Errorf("empty slot handling: %v", gap)
	}
}

func TestSplitDuplexSheets(t *testing.T) {
	tests := []struct {
		pages        []int
		per          int
		reverse      bool
		fronts, back []int
	}{
		{[]int{1, 2, 3, 4, 5}, 1, false, []int{1, 3, 5}, []int{2, 4}},
		{[]int{1, 2, 3, 4, 5}, 1, true, []int{1, 3, 5}, []int{4, 2}},
		{[]int{1, 2, 3, 4, 5, 6, 7, 8}, 2, false, []int{1, 2, 5, 6}, []int{3, 4, 7, 8}},
		// The short last sheet is padded so reversing keeps pairs together.
		{[]int{1, 2, 3, 4, 5, 6, 7}, 2, true, []int{1, 2, 5, 6}, []int{7, 0, 3, 4}},
	}
	for _, tc := range tests {
		f, b := SplitDuplexSheets(tc.pages, tc.per, tc.reverse)
		if !reflect.DeepEqual(f, tc.fronts) || !reflect.DeepEqual(b, tc.back) {
			t.Errorf("SplitDuplexSheets(%v, %d, %v) = %v, %v; want %v, %v", tc.pages, tc.per, tc.reverse, f, b, tc.fronts, tc.back)
		}
	}
}

func TestLayoutPhotoRepeat(t *testing.T) {
	photo := sizes(map[int][2]float64{1: {6, 4}}) // landscape photo
	for _, n := range []int{2, 3, 4, 6, 8} {
		o := Options{Repeat: n}
		orient := SheetOrientation(o, 6, 4)
		w, h := areaW, areaH
		if orient == Landscape {
			w, h = h, w
		}
		sheets := Layout([]int{1}, photo, w, h, o)
		if len(sheets) != 1 || len(sheets[0]) != n {
			t.Fatalf("repeat %d: %d sheets, %d copies", n, len(sheets), len(sheets[0]))
		}
		for i, a := range sheets[0] {
			if a.Page != 1 {
				t.Errorf("repeat %d: slot %d has page %d", n, i, a.Page)
			}
			for _, b := range sheets[0][i+1:] {
				if a.Rect.X < b.Rect.X+b.Rect.W-0.001 && b.Rect.X < a.Rect.X+a.Rect.W-0.001 &&
					a.Rect.Y < b.Rect.Y+b.Rect.H-0.001 && b.Rect.Y < a.Rect.Y+a.Rect.H-0.001 {
					t.Errorf("repeat %d: copies overlap", n)
				}
			}
		}
	}
	// Three landscape photos stack best on a portrait sheet.
	if got := SheetOrientation(Options{Repeat: 3}, 6, 4); got != Portrait {
		t.Errorf("3 landscape copies: %q, want portrait", got)
	}
	// Copies stay upright: 4 landscape photos go on a landscape sheet.
	o4 := Options{Repeat: 4}
	if got := SheetOrientation(o4, 6, 4); got != Landscape {
		t.Errorf("4 landscape copies: %q, want landscape", got)
	}
	for _, pl := range Layout([]int{1}, photo, areaH, areaW, o4)[0] {
		if pl.Rotate != 0 {
			t.Errorf("4 landscape copies turned %d", pl.Rotate)
		}
	}
	// Each source page still takes one sheet.
	if got := Sheets(3, Options{Repeat: 4}); got != 3 {
		t.Errorf("Sheets with repeat = %d, want 3", got)
	}
}

func TestLayoutScaleAndOffset(t *testing.T) {
	size := sizes(nil)
	full := Layout([]int{1}, size, areaW, areaH, Options{})[0][0]
	half := Layout([]int{1}, size, areaW, areaH, Options{Scale: 50})[0][0]
	if !near(half.Rect.W, full.Rect.W/2) || !near(half.Rect.H, full.Rect.H/2) {
		t.Errorf("50%% scale: %+v vs %+v", half.Rect, full.Rect)
	}
	if !near(half.Rect.X+half.Rect.W/2, areaW/2) {
		t.Errorf("not centred by default: %+v", half.Rect)
	}
	topLeft := Layout([]int{1}, size, areaW, areaH, Options{Scale: 50, OffX: -1, OffY: -1})[0][0]
	if !near(topLeft.Rect.X, 0) || !near(topLeft.Rect.Y, 0) {
		t.Errorf("offset -1,-1 should touch the top-left: %+v", topLeft.Rect)
	}
	bottomRight := Layout([]int{1}, size, areaW, areaH, Options{Scale: 50, OffX: 5, OffY: 1})[0][0]
	if !near(bottomRight.Rect.X+bottomRight.Rect.W, areaW) || !near(bottomRight.Rect.Y+bottomRight.Rect.H, areaH) {
		t.Errorf("offset 1,1 (clamped) should touch the bottom-right: %+v", bottomRight.Rect)
	}
}

func TestLayoutOneSheet(t *testing.T) {
	land := [2]float64{6, 4}
	port := [2]float64{4, 6}
	inside := func(t *testing.T, sh Sheet) {
		t.Helper()
		for _, pl := range sh {
			c := pl.Cell
			if c.X < -0.001 || c.Y < -0.001 || c.X+c.W > areaW+0.001 || c.Y+c.H > areaH+0.001 {
				t.Errorf("page %d outside the paper: %+v", pl.Page, c)
			}
		}
	}
	t.Run("four portrait photos make a 2x2 grid", func(t *testing.T) {
		size := sizes(map[int][2]float64{1: port, 2: port, 3: port, 4: port})
		sheets := Layout([]int{1, 2, 3, 4}, size, areaW, areaH, Options{OneSheet: true})
		if len(sheets) != 1 || len(sheets[0]) != 4 {
			t.Fatalf("got %d sheets", len(sheets))
		}
		sh := sheets[0]
		inside(t, sh)
		if !near(sh[0].Cell.Y, sh[1].Cell.Y) || near(sh[0].Cell.Y, sh[2].Cell.Y) {
			t.Errorf("not two rows of two: %+v", sh)
		}
	})
	t.Run("three landscape photos stack on portrait paper", func(t *testing.T) {
		size := sizes(map[int][2]float64{1: land, 2: land, 3: land})
		sh := Layout([]int{1, 2, 3}, size, areaW, areaH, Options{OneSheet: true})[0]
		inside(t, sh)
		for i, pl := range sh {
			if pl.Rotate != 0 || !near(pl.Rect.X+pl.Rect.W/2, areaW/2) || !near(pl.Rect.H, (areaH-2*cellGap)/3) {
				t.Errorf("photo %d: rotate %d at %+v, want upright, one column, a third of the height", i+1, pl.Rotate, pl.Rect)
			}
		}
	})
	t.Run("a short last row is centred", func(t *testing.T) {
		size := sizes(map[int][2]float64{1: port, 2: port, 3: port})
		sh := Layout([]int{1, 2, 3}, size, areaW, areaH, Options{OneSheet: true})[0]
		mid := sh[2].Cell.X + sh[2].Cell.W/2
		if !near(mid, areaW/2) {
			t.Errorf("last photo centre %.2f, want %.2f", mid, areaW/2)
		}
	})
	t.Run("the order given is the order placed", func(t *testing.T) {
		sh := Layout([]int{3, 1, 2}, sizes(nil), areaW, areaH, Options{OneSheet: true})[0]
		if sh[0].Page != 3 || sh[1].Page != 1 || sh[2].Page != 2 {
			t.Errorf("order %d %d %d", sh[0].Page, sh[1].Page, sh[2].Page)
		}
	})
	t.Run("a moved photo goes where it was put, inside the paper", func(t *testing.T) {
		size := sizes(map[int][2]float64{1: port, 2: port})
		o := Options{OneSheet: true, Scale: 50, Place: map[int][2]float64{2: {0.5, 0.5}, 1: {1, 1}}}
		sh := Layout([]int{1, 2}, size, areaW, areaH, o)[0]
		inside(t, sh)
		c := sh[1].Cell
		if !near(c.X+c.W/2, areaW/2) || !near(c.Y+c.H/2, areaH/2) {
			t.Errorf("photo 2 at %+v, want centred", c)
		}
		if !near(c.W, sh[1].Rect.W) || !near(c.H, sh[1].Rect.H) {
			t.Errorf("box %+v isn't the photo %+v", c, sh[1].Rect)
		}
		if c1 := sh[0].Cell; !near(c1.X+c1.W, areaW) || !near(c1.Y+c1.H, areaH) {
			t.Errorf("photo 1 at %+v, want in the bottom-right corner", c1)
		}
	})
	t.Run("one sheet counts as one sheet", func(t *testing.T) {
		o := Options{OneSheet: true}
		if n := Sheets(12, o); n != 1 {
			t.Errorf("Sheets = %d", n)
		}
	})
}

func TestPickOrientation(t *testing.T) {
	land := [2]float64{6, 4}
	size := sizes(map[int][2]float64{1: land, 2: land})
	if got := PickOrientation(Options{OneSheet: true}, []int{1, 2}, size); got != Portrait {
		t.Errorf("two landscape photos: %q, want portrait (stacked)", got)
	}
	size = sizes(map[int][2]float64{1: {4, 6}, 2: {4, 6}})
	if got := PickOrientation(Options{OneSheet: true}, []int{1, 2}, size); got != Landscape {
		t.Errorf("two portrait photos: %q, want landscape (side by side)", got)
	}
	if got := PickOrientation(Options{PerSheet: 3}, []int{1}, sizes(map[int][2]float64{1: land})); got != Portrait {
		t.Errorf("3 per sheet of landscape pages: %q, want portrait", got)
	}
	if got := PickOrientation(Options{OneSheet: true, Orientation: Landscape}, []int{1, 2}, size); got != Landscape {
		t.Errorf("a chosen orientation is kept: %q", got)
	}
}

func TestPerSheetThreeAndEight(t *testing.T) {
	for _, n := range []int{3, 8} {
		if PerSheet(Options{PerSheet: n}) != n {
			t.Errorf("PerSheet(%d) not allowed", n)
		}
		sheets := Layout([]int{1, 2, 3, 4, 5, 6, 7, 8, 9}, sizes(nil), areaW, areaH, Options{PerSheet: n})
		if want := (9 + n - 1) / n; len(sheets) != want {
			t.Errorf("%d per sheet: %d sheets, want %d", n, len(sheets), want)
		}
	}
}
