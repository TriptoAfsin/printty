package printer

import "math"

// The layout engine decides where every page goes on every sheet. It knows
// nothing about Windows or CUPS: backends call it with the page sizes and the
// printable area and draw what it returns, so every OS prints the same way.
// The web editor's preview (app.js, layoutSheets) mirrors these rules.

// Placement is one page on a sheet. Rect is in inches, relative to the
// printable area's top-left corner. Cell is the space the page may use:
// anything outside it (Fill, Actual) is trimmed.
type Placement struct {
	Page   int // 1-based
	Rotate int // clockwise degrees: 0, 90, 180, 270
	Rect   Rect
	Cell   Rect
}

// Rect is a rectangle in inches.
type Rect struct{ X, Y, W, H float64 }

// Sheet is one side of paper.
type Sheet []Placement

const cellGap = 0.12 // inches between pages when several share a sheet

// grid returns columns and rows for n pages per sheet, on a sheet that is
// wider than tall or not.
func grid(n int, wide bool) (cols, rows int) {
	long, short := 1, 1
	switch n {
	case 2:
		long, short = 2, 1
	case 3:
		long, short = 3, 1
	case 4:
		long, short = 2, 2
	case 6:
		long, short = 3, 2
	case 8:
		long, short = 4, 2
	case 9:
		long, short = 3, 3
	}
	if wide {
		return long, short
	}
	return short, long
}

// PerSheet normalises the pages-per-sheet option.
func PerSheet(o Options) int {
	switch o.PerSheet {
	case 2, 4, 6, 9:
		return o.PerSheet
	}
	return 1
}

// RepeatCount normalises the copies-on-one-sheet option (1 = off).
func RepeatCount(o Options) int {
	switch o.Repeat {
	case 2, 3, 4, 6, 8:
		return o.Repeat
	}
	return 1
}

// slots is how many places a sheet has: repeated copies or pages per sheet.
func slots(o Options) int {
	if r := RepeatCount(o); r > 1 {
		return r
	}
	return PerSheet(o)
}

// SheetSize is how many different source pages go on one sheet.
func SheetSize(o Options) int {
	if RepeatCount(o) > 1 {
		return 1
	}
	return PerSheet(o)
}

func clamp1(v float64) float64 { return math.Max(-1, math.Min(1, v)) }

// SheetOrientation picks the paper orientation for a job. Auto: one page per
// sheet stays portrait (pages are turned to fit instead); 2 or 6 per sheet
// turns the sheet so the cells match the first page's shape.
func SheetOrientation(o Options, firstW, firstH float64) Orientation {
	if o.Orientation == Portrait || o.Orientation == Landscape {
		return o.Orientation
	}
	if r := RepeatCount(o); r > 1 {
		// Copies of one photo: whichever way makes each copy bigger.
		if bestScale(r, firstW, firstH, true) > bestScale(r, firstW, firstH, false) {
			return Landscape
		}
		return Portrait
	}
	n := PerSheet(o)
	if n == 2 || n == 6 {
		if firstW > firstH {
			return Portrait // landscape pages stack on a portrait sheet
		}
		return Landscape
	}
	return Portrait
}

// bestScale is how big an upright w×h page gets in an n-slot grid on an
// A4-shaped sheet; used only to compare orientations.
func bestScale(n int, w, h float64, wide bool) float64 {
	sw, sh := 8.0, 11.4
	if wide {
		sw, sh = sh, sw
	}
	cols, rows := grid(n, wide)
	cw := (sw - cellGap*float64(cols-1)) / float64(cols)
	ch := (sh - cellGap*float64(rows-1)) / float64(rows)
	// Upright: photos print the right way up when either way is close.
	return math.Min(cw/w, ch/h)
}

// Layout places pages onto sheets. pages may contain 0 for an empty slot.
// size returns a page's size in inches; areaW, areaH is the printable area of
// the sheet in its final orientation.
func Layout(pages []int, size func(page int) (w, h float64), areaW, areaH float64, o Options) []Sheet {
	n := slots(o)
	if r := RepeatCount(o); r > 1 {
		// Each page fills a whole sheet with r copies of itself.
		var rep []int
		for _, p := range pages {
			for i := 0; i < r; i++ {
				rep = append(rep, p)
			}
		}
		pages = rep
	}
	cols, rows := grid(n, areaW > areaH)
	gap := 0.0
	if n > 1 {
		gap = cellGap
	}
	cw := (areaW - gap*float64(cols-1)) / float64(cols)
	ch := (areaH - gap*float64(rows-1)) / float64(rows)

	var sheets []Sheet
	for start := 0; start < len(pages); start += n {
		sh := Sheet{}
		for i := 0; i < n && start+i < len(pages); i++ {
			p := pages[start+i]
			if p <= 0 {
				continue
			}
			cell := Rect{X: float64(i%cols) * (cw + gap), Y: float64(i/cols) * (ch + gap), W: cw, H: ch}
			w, h := size(p)
			if w <= 0 || h <= 0 {
				w, h = 8.27, 11.69
			}
			rot := pageRotation(p, w, h, cell, o)
			if rot == 90 || rot == 270 {
				w, h = h, w
			}
			var scale float64
			switch o.Fit {
			case Fill:
				scale = math.Max(cell.W/w, cell.H/h)
			case Actual:
				scale = 1
			default:
				scale = math.Min(cell.W/w, cell.H/h)
			}
			if o.Scale > 0 && o.Scale < 100 {
				scale *= float64(max(o.Scale, 10)) / 100
			}
			dw, dh := w*scale, h*scale
			// Offsets move the page within its free space (or pan it when
			// it's bigger than its space); 0 is centred.
			fx, fy := (1+clamp1(o.OffX))/2, (1+clamp1(o.OffY))/2
			sh = append(sh, Placement{
				Page: p, Rotate: rot, Cell: cell,
				Rect: Rect{X: cell.X + (cell.W-dw)*fx, Y: cell.Y + (cell.H-dh)*fy, W: dw, H: dh},
			})
		}
		sheets = append(sheets, sh)
	}
	return sheets
}

// pageRotation is the person's choice for this page if they made one, else a
// quarter turn when the page's shape doesn't match its space (auto-rotate).
func pageRotation(p int, w, h float64, cell Rect, o Options) int {
	if r, ok := o.Rotate[p]; ok {
		return NormRotation(r)
	}
	if o.NoAutoRotate {
		return 0
	}
	if (w > h) != (cell.W > cell.H) && math.Abs(w-h) > 0.01 {
		return 90
	}
	return 0
}

// NormRotation turns any angle into 0, 90, 180 or 270.
func NormRotation(r int) int {
	r %= 360
	if r < 0 {
		r += 360
	}
	return r / 90 * 90
}

// Sheets is how many sheets (sides) n pages need.
func Sheets(pages int, o Options) int {
	n := SheetSize(o)
	return (pages + n - 1) / n
}

// SplitDuplexSheets splits pages for a manual flip, sheet by sheet: fronts
// are the odd sheets, backs the even ones. With several pages per sheet each
// sheet keeps its pages together; a short last sheet is padded with 0 (empty
// slots) so reversing the backs doesn't regroup them.
func SplitDuplexSheets(pages []int, perSheet int, reverseBacks bool) (fronts, backs []int) {
	if perSheet < 1 {
		perSheet = 1
	}
	var groups [][]int
	for i := 0; i < len(pages); i += perSheet {
		g := make([]int, perSheet)
		copy(g, pages[i:min(i+perSheet, len(pages))])
		groups = append(groups, g)
	}
	var fg, bg [][]int
	for i, g := range groups {
		if i%2 == 0 {
			fg = append(fg, g)
		} else {
			bg = append(bg, g)
		}
	}
	if reverseBacks {
		for i, j := 0, len(bg)-1; i < j; i, j = i+1, j-1 {
			bg[i], bg[j] = bg[j], bg[i]
		}
	}
	flat := func(gs [][]int) []int {
		var out []int
		for _, g := range gs {
			out = append(out, g...)
		}
		// Trailing empty slots aren't needed.
		for len(out) > 0 && out[len(out)-1] == 0 {
			out = out[:len(out)-1]
		}
		return out
	}
	return flat(fg), flat(bg)
}
