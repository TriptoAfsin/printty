//go:build windows

package printer

import (
	"context"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strconv"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Sheets, driver options and previews for the Windows backend. The page
// placement itself comes from layout.go, shared with every OS.

var (
	procSaveDC            = modGdi32.NewProc("SaveDC")
	procRestoreDC         = modGdi32.NewProc("RestoreDC")
	procIntersectClipRect = modGdi32.NewProc("IntersectClipRect")
)

const (
	dcBins           = 6
	dcBinNames       = 12
	dcMediaTypeNames = 34
	dcMediaTypes     = 35

	dmFieldOrientation   = 0x1
	dmFieldDefaultSource = 0x200
	dmFieldPrintQuality  = 0x400
	dmFieldMediaType     = 0x2000000

	dmOrientPortrait  = 1
	dmOrientLandscape = 2

	maxRenderPixels = 40_000_000 // one page bitmap stays under ~160 MB
)

// Quality choices map to the standard DEVMODE print qualities, which drivers
// translate to their own modes (the Canon G1010 driver keeps High and turns
// Draft into its low-quality, 300 dpi mode). Pages are also rendered at a
// resolution to match: see renderDPIFor.
var qualityValues = map[string]int16{"draft": -1, "standard": -3, "high": -4}

// defaultDevmode reads the printer's default settings.
func defaultDevmode(name string) *devmodeW {
	h, err := openPrinter(name)
	if err != nil {
		return nil
	}
	defer closePrinter(h)
	pname := utf16(name)
	size, _, _ := procDocumentPropertiesW.Call(0, uintptr(h), uintptr(unsafe.Pointer(pname)), 0, 0, 0)
	if int32(size) <= 0 {
		return nil
	}
	buf := make([]byte, size)
	r, _, _ := procDocumentPropertiesW.Call(0, uintptr(h), uintptr(unsafe.Pointer(pname)), uintptr(unsafe.Pointer(&buf[0])), 0, dmOutBuffer)
	if int32(r) < 0 {
		return nil
	}
	return (*devmodeW)(unsafe.Pointer(&buf[0]))
}

// driverOptions reads the printer's paper types, paper sources and print
// quality from its driver, in the generic DriverOption form.
func driverOptions(name string) []DriverOption {
	dm := defaultDevmode(name)
	var opts []DriverOption

	if n := deviceCaps(name, dcMediaTypes, nil); n > 0 && n < 256 {
		ids := make([]uint32, n)
		names := make([]uint16, int(n)*64)
		deviceCaps(name, dcMediaTypes, unsafe.Pointer(&ids[0]))
		deviceCaps(name, dcMediaTypeNames, unsafe.Pointer(&names[0]))
		o := DriverOption{Key: OptMedia, Label: "Paper type"}
		for i := 0; i < int(n); i++ {
			label := windows.UTF16ToString(names[i*64 : (i+1)*64])
			if label == "" {
				continue
			}
			o.Choices = append(o.Choices, Choice{Value: strconv.FormatUint(uint64(ids[i]), 10), Label: label})
		}
		if dm != nil && dm.Fields&dmFieldMediaType != 0 {
			o.Default = strconv.FormatUint(uint64(dm.MediaType), 10)
		}
		if len(o.Choices) > 1 {
			if !o.Valid(o.Default) {
				o.Default = o.Choices[0].Value
			}
			opts = append(opts, o)
		}
	}

	if n := deviceCaps(name, dcBins, nil); n > 0 && n < 256 {
		ids := make([]uint16, n)
		names := make([]uint16, int(n)*24)
		deviceCaps(name, dcBins, unsafe.Pointer(&ids[0]))
		deviceCaps(name, dcBinNames, unsafe.Pointer(&names[0]))
		o := DriverOption{Key: "source", Label: "Paper source"}
		for i := 0; i < int(n); i++ {
			label := windows.UTF16ToString(names[i*24 : (i+1)*24])
			if label == "" {
				continue
			}
			o.Choices = append(o.Choices, Choice{Value: strconv.Itoa(int(ids[i])), Label: label})
		}
		if dm != nil && dm.Fields&dmFieldDefaultSource != 0 {
			o.Default = strconv.Itoa(int(dm.DefaultSource))
		}
		if len(o.Choices) > 1 {
			if !o.Valid(o.Default) {
				o.Default = o.Choices[0].Value
			}
			opts = append(opts, o)
		}
	}

	opts = append(opts, DriverOption{Key: OptQuality, Label: "Print quality", Default: "standard", Choices: []Choice{
		{Value: "draft", Label: "Draft"},
		{Value: "standard", Label: "Standard"},
		{Value: "high", Label: "High"},
	}})
	return opts
}

// applyLayoutAndDriver merges sheet orientation and driver options into dm.
func applyLayoutAndDriver(dm *devmodeW, orient Orientation, o Options) {
	dm.Fields |= dmFieldOrientation
	dm.Orientation = dmOrientPortrait
	if orient == Landscape {
		dm.Orientation = dmOrientLandscape
	}
	if v, ok := o.Driver["media"]; ok {
		if id, err := strconv.ParseUint(v, 10, 32); err == nil {
			dm.Fields |= dmFieldMediaType
			dm.MediaType = uint32(id)
		}
	}
	if v, ok := o.Driver["source"]; ok {
		if id, err := strconv.ParseInt(v, 10, 16); err == nil {
			dm.Fields |= dmFieldDefaultSource
			dm.DefaultSource = int16(id)
		}
	}
	if q, ok := qualityValues[o.Driver["quality"]]; ok {
		dm.Fields |= dmFieldPrintQuality
		dm.PrintQuality = q
	}
}

// drawSheet prints one sheet: each page rendered, turned and placed as the
// layout says, trimmed to its cell.
func drawSheet(hdc uintptr, src *pageSource, sheet Sheet, dpiX, dpiY int, maxDPI float64) error {
	if r, _, _ := procStartPage.Call(hdc); int32(r) <= 0 {
		return fmt.Errorf("StartPage failed")
	}
	for _, pl := range sheet {
		if err := drawPlacement(hdc, src, pl, dpiX, dpiY, maxDPI); err != nil {
			return fmt.Errorf("page %d: %w", pl.Page, err)
		}
	}
	if r, _, _ := procEndPage.Call(hdc); int32(r) <= 0 {
		return fmt.Errorf("EndPage failed")
	}
	return nil
}

func drawPlacement(hdc uintptr, src *pageSource, pl Placement, dpiX, dpiY int, maxDPI float64) error {
	px := func(in float64, dpi int) int { return int(in*float64(dpi) + 0.5) }
	destX, destY := px(pl.Rect.X, dpiX), px(pl.Rect.Y, dpiY)
	destW, destH := px(pl.Rect.W, dpiX), px(pl.Rect.H, dpiY)
	if destW <= 0 || destH <= 0 {
		return nil
	}
	// Render the page upright at up to maxDPI, then turn it.
	rdpi := min(float64(dpiX), maxDPI)
	uw, uh := pl.Rect.W, pl.Rect.H
	if pl.Rotate == 90 || pl.Rotate == 270 {
		uw, uh = uh, uw
	}
	if pix := uw * rdpi * uh * rdpi; pix > maxRenderPixels {
		rdpi *= sqrt(maxRenderPixels / pix)
	}
	img, err := src.draw(pl.Page-1, max(1, int(uw*rdpi)), max(1, int(uh*rdpi)))
	if err != nil {
		return err
	}
	img = rotateImage(img, pl.Rotate)
	b := img.Bounds()

	procSaveDC.Call(hdc)
	defer procRestoreDC.Call(hdc, ^uintptr(0)) // -1: the last saved state
	c := pl.Cell
	procIntersectClipRect.Call(hdc, uintptr(px(c.X, dpiX)), uintptr(px(c.Y, dpiY)), uintptr(px(c.X+c.W, dpiX)), uintptr(px(c.Y+c.H, dpiY)))

	// Send in bands of rows to keep each GDI call and buffer small.
	band := make([]byte, b.Dx()*4*bandRows)
	for y0 := b.Min.Y; y0 < b.Max.Y; y0 += bandRows {
		rows := min(bandRows, b.Max.Y-y0)
		fillBGRA(band, img, y0, rows)
		bih := bitmapInfoHeader{Width: int32(b.Dx()), Height: -int32(rows), Planes: 1, BitCount: 32}
		bih.Size = uint32(unsafe.Sizeof(bih))
		dy0 := destY + (y0-b.Min.Y)*destH/b.Dy()
		dy1 := destY + (y0-b.Min.Y+rows)*destH/b.Dy()
		procStretchDIBits.Call(hdc,
			uintptr(destX), uintptr(dy0), uintptr(destW), uintptr(dy1-dy0),
			0, 0, uintptr(b.Dx()), uintptr(rows),
			uintptr(unsafe.Pointer(&band[0])), uintptr(unsafe.Pointer(&bih)), dibRGBColors, srcCopy)
	}
	return nil
}

func sqrt(x float64) float64 {
	// Newton's method is plenty for a scale factor.
	z := x
	for i := 0; i < 20; i++ {
		z -= (z*z - x) / (2 * z)
	}
	return z
}

// rotateImage turns img clockwise by 0, 90, 180 or 270 degrees.
func rotateImage(src image.Image, deg int) image.Image {
	deg = NormRotation(deg)
	if deg == 0 {
		return src
	}
	in := toNRGBA(src)
	b := in.Bounds()
	w, h := b.Dx(), b.Dy()
	var out *image.NRGBA
	if deg == 180 {
		out = image.NewNRGBA(image.Rect(0, 0, w, h))
	} else {
		out = image.NewNRGBA(image.Rect(0, 0, h, w))
	}
	for y := 0; y < h; y++ {
		row := in.Pix[y*in.Stride:]
		for x := 0; x < w; x++ {
			var dx, dy int
			switch deg {
			case 90:
				dx, dy = h-1-y, x
			case 180:
				dx, dy = w-1-x, h-1-y
			case 270:
				dx, dy = y, w-1-x
			}
			copy(out.Pix[dy*out.Stride+dx*4:dy*out.Stride+dx*4+4], row[x*4:x*4+4])
		}
	}
	return out
}

func toNRGBA(src image.Image) *image.NRGBA {
	if n, ok := src.(*image.NRGBA); ok && n.Bounds().Min == (image.Point{}) {
		return n
	}
	b := src.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			r, g, bl, a := src.At(b.Min.X+x, b.Min.Y+y).RGBA()
			i := y*out.Stride + x*4
			if a == 0 {
				continue
			}
			out.Pix[i] = uint8(r * 0xffff / a >> 8)
			out.Pix[i+1] = uint8(g * 0xffff / a >> 8)
			out.Pix[i+2] = uint8(bl * 0xffff / a >> 8)
			out.Pix[i+3] = uint8(a >> 8)
		}
	}
	return out
}

// Inspect counts pages and writes previews dir/1.png... for the first max pages.
func (w *Windows) Inspect(ctx context.Context, path, kind, dir string, max int) (int, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return 0, err
	}
	src := Job{Path: path, Kind: kind}
	n := 0
	err := withWinRT(func() error {
		s, err := openSource(src)
		if err != nil {
			return err
		}
		defer s.close()
		n = s.count
		for i := 0; i < s.count && i < max; i++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			wIn, hIn := s.size(i)
			scale := thumbSize / maxf(wIn, hIn)
			img, err := s.draw(i, int(wIn*scale+0.5), int(hIn*scale+0.5))
			if err != nil {
				return err
			}
			if b := img.Bounds(); b.Dx() > thumbSize*2 || b.Dy() > thumbSize*2 {
				img = shrink(img, int(wIn*scale+0.5), int(hIn*scale+0.5)) // photos come back full size
			}
			if err := writePNG(filepath.Join(dir, strconv.Itoa(i+1)+".png"), img); err != nil {
				return err
			}
		}
		return nil
	})
	return n, err
}
