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

// turnedWeight is how much a page turned by auto-rotate counts when packing
// pages onto one sheet (see packGrid).
const turnedWeight = 0.9

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
	case 2, 3, 4, 6, 8, 9:
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
	if o.OneSheet {
		return MaxPhotos
	}
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
	if n == 2 || n == 3 || n == 6 || n == 8 {
		if firstW > firstH {
			return Portrait // landscape pages stack on a portrait sheet
		}
		return Landscape
	}
	return Portrait
}

// PickOrientation is SheetOrientation for a whole job: with OneSheet it
// takes whichever orientation shows all the pages biggest.
func PickOrientation(o Options, pages []int, size func(page int) (w, h float64)) Orientation {
	ps := realPages(pages)
	if !o.OneSheet || o.Orientation == Portrait || o.Orientation == Landscape {
		first := 1
		if len(ps) > 0 {
			first = ps[0]
		}
		fw, fh := pageSize(first, size)
		return SheetOrientation(o, fw, fh)
	}
	_, _, port := packGrid(ps, size, 8, 11.4, o)
	_, _, land := packGrid(ps, size, 11.4, 8, o)
	if land > port*1.0001 {
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

// Binding margin sides and limits (Options.BindSide, BindMM).
const (
	BindLeft  = "left"
	BindRight = "right"
	BindMinMM = 5
	BindMaxMM = 30
)

// bindInset is how much of the printable area the binding margin takes, in
// inches: the margin less the printer's own edge, at most half the area.
func bindInset(o Options, areaW float64) float64 {
	if (o.BindSide != BindLeft && o.BindSide != BindRight) || o.BindMM <= 0 {
		return 0
	}
	return math.Max(0, math.Min(areaW/2, float64(o.BindMM)/25.4-o.BindEdge))
}

// BindOnLeft reports whether sheet i (0-based, in printing order) has its
// binding margin on the left. A back side is mirrored when the sheet flips
// over its side edge: the long edge of a portrait sheet or the short edge of
// a landscape one. With the OS duplexing, every second sheet is a back; a
// manual back pass (BackFlip) is all backs.
func BindOnLeft(o Options, i int, wide bool) bool {
	left := o.BindSide == BindLeft
	flip := o.BackFlip
	if flip == "" && (o.Duplex == LongEdge || o.Duplex == ShortEdge) && i%2 == 1 {
		flip = o.Duplex
	}
	if flip != "" && (flip == LongEdge) != wide {
		left = !left
	}
	return left
}

// Layout places pages onto sheets. pages may contain 0 for an empty slot.
// size returns a page's size in inches; areaW, areaH is the printable area of
// the sheet in its final orientation. A binding margin narrows the area and,
// on sheets where it's on the left, moves everything right.
func Layout(pages []int, size func(page int) (w, h float64), areaW, areaH float64, o Options) []Sheet {
	g := bindInset(o, areaW)
	if g == 0 {
		return layoutArea(pages, size, areaW, areaH, o)
	}
	sheets := layoutArea(pages, size, areaW-g, areaH, o)
	for i, sh := range sheets {
		if !BindOnLeft(o, i, areaW > areaH) {
			continue
		}
		for k := range sh {
			sh[k].Rect.X += g
			sh[k].Cell.X += g
		}
	}
	return sheets
}

func layoutArea(pages []int, size func(page int) (w, h float64), areaW, areaH float64, o Options) []Sheet {
	if o.OneSheet {
		return []Sheet{oneSheet(realPages(pages), size, areaW, areaH, o)}
	}
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
			sh = append(sh, placeIn(p, cell, size, o, o.OffX, o.OffY))
		}
		sheets = append(sheets, sh)
	}
	return sheets
}

// placeIn sizes and turns page p for its cell. offX, offY move it within its
// free space (or pan it when it's bigger than its space); 0 is centred.
func placeIn(p int, cell Rect, size func(int) (float64, float64), o Options, offX, offY float64) Placement {
	w, h := pageSize(p, size)
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
	fx, fy := (1+clamp1(offX))/2, (1+clamp1(offY))/2
	return Placement{
		Page: p, Rotate: rot, Cell: cell,
		Rect: Rect{X: cell.X + (cell.W-dw)*fx, Y: cell.Y + (cell.H-dh)*fy, W: dw, H: dh},
	}
}

func pageSize(p int, size func(int) (float64, float64)) (float64, float64) {
	w, h := size(p)
	if w <= 0 || h <= 0 {
		return 8.27, 11.69
	}
	return w, h
}

func realPages(pages []int) []int {
	var out []int
	for _, p := range pages {
		if p > 0 {
			out = append(out, p)
		}
	}
	return out
}

// packGrid finds the columns and rows that show the pages biggest on an
// areaW×areaH sheet: the most paper covered at Fit, a page turned to fit
// counting a little less so photos stay upright when it's close. Ties go to
// fewer columns.
// The editor's preview (editor.js, packGrid) mirrors it.
func packGrid(pages []int, size func(int) (float64, float64), areaW, areaH float64, o Options) (cols, rows int, score float64) {
	n := len(pages)
	if n == 0 {
		return 1, 1, 0
	}
	cols, rows = 1, n
	for c := 1; c <= n; c++ {
		r := (n + c - 1) / c
		if c > 1 && (n+c-2)/(c-1) == r {
			continue // the same rows with fewer columns was already tried
		}
		cell := gridCell(c, r, areaW, areaH, n)
		total := 0.0
		for _, p := range pages {
			w, h := pageSize(p, size)
			weight := 1.0
			if rot := pageRotation(p, w, h, cell, o); rot == 90 || rot == 270 {
				w, h = h, w
				if _, chosen := o.Rotate[p]; !chosen {
					weight = turnedWeight
				}
			}
			k := math.Min(cell.W/w, cell.H/h)
			total += w * h * k * k * weight
		}
		if total > score*1.0001 {
			cols, rows, score = c, r, total
		}
	}
	return cols, rows, score
}

// gridCell is the size of one cell in a cols×rows grid holding n pages.
func gridCell(cols, rows int, areaW, areaH float64, n int) Rect {
	gap := 0.0
	if n > 1 {
		gap = cellGap
	}
	return Rect{W: (areaW - gap*float64(cols-1)) / float64(cols), H: (areaH - gap*float64(rows-1)) / float64(rows)}
}

// oneSheet lays every page out on one sheet in the best grid, with the last
// row centred. A page's Cell is the part of it that shows; a page in o.Place
// is moved there, kept inside the printable area.
func oneSheet(pages []int, size func(int) (float64, float64), areaW, areaH float64, o Options) Sheet {
	cols, rows, _ := packGrid(pages, size, areaW, areaH, o)
	n := len(pages)
	gap := 0.0
	if n > 1 {
		gap = cellGap
	}
	c := gridCell(cols, rows, areaW, areaH, n)
	sh := Sheet{}
	for i, p := range pages {
		row, col := i/cols, i%cols
		inRow := min(cols, n-row*cols)
		shift := float64(cols-inRow) * (c.W + gap) / 2
		cell := Rect{X: shift + float64(col)*(c.W+gap), Y: float64(row) * (c.H + gap), W: c.W, H: c.H}
		pl := placeIn(p, cell, size, o, 0, 0)
		pl.Cell = intersect(pl.Cell, pl.Rect)
		if at, ok := o.Place[p]; ok {
			nx := math.Max(0, math.Min(areaW-pl.Cell.W, clamp01(at[0])*areaW-pl.Cell.W/2))
			ny := math.Max(0, math.Min(areaH-pl.Cell.H, clamp01(at[1])*areaH-pl.Cell.H/2))
			dx, dy := nx-pl.Cell.X, ny-pl.Cell.Y
			pl.Cell.X, pl.Cell.Y = nx, ny
			pl.Rect.X, pl.Rect.Y = pl.Rect.X+dx, pl.Rect.Y+dy
		}
		sh = append(sh, pl)
	}
	return sh
}

func clamp01(v float64) float64 { return math.Max(0, math.Min(1, v)) }

func intersect(a, b Rect) Rect {
	x0, y0 := math.Max(a.X, b.X), math.Max(a.Y, b.Y)
	x1, y1 := math.Min(a.X+a.W, b.X+b.W), math.Min(a.Y+a.H, b.Y+b.H)
	return Rect{X: x0, Y: y0, W: math.Max(0, x1-x0), H: math.Max(0, y1-y0)}
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
