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
