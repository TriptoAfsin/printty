package printer

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg" // decoders for photos
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"strconv"
)

// Several photos sent together become one PDF with a page per photo, so the
// editor, the layout engine and every backend treat them like any document.
// JPEGs are embedded as they are (no re-encoding); PNGs are stored losslessly.

// MaxPhotos is how many photos one upload may combine.
const MaxPhotos = 30

// photoDPI is the resolution a photo is assumed to have, as for single photos.
const photoDPI = 300

// Composer is an optional Backend feature: combine photos into one PDF and
// write previews (dir/1.png...) for the first max of them.
type Composer interface {
	Compose(ctx context.Context, paths []string, out, dir string, max int) (pages int, err error)
}

// ComposePhotos writes the photos at paths, in order, to a new PDF at out,
// one per page, and previews of the first max into dir. It returns the page count.
func ComposePhotos(ctx context.Context, paths []string, out, dir string, max int) (int, error) {
	if len(paths) == 0 {
		return 0, errors.New("no photos")
	}
	if len(paths) > MaxPhotos {
		return 0, fmt.Errorf("at most %d photos at once", MaxPhotos)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return 0, err
	}
	f, err := os.OpenFile(out, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, err
	}
	pw := &pdfWriter{w: bufio.NewWriterSize(f, 64<<10)}
	pw.printf("%%PDF-1.4\n%%\xe2\xe3\xcf\xd3\n")
	pw.reserve(2) // 1 catalog, 2 page tree; written last
	var kids []int
	for i, p := range paths {
		if err := ctx.Err(); err != nil {
			f.Close()
			os.Remove(out)
			return 0, err
		}
		page, err := pw.addPhoto(p, i < max, filepath.Join(dir, strconv.Itoa(i+1)+".png"))
		if err != nil {
			f.Close()
			os.Remove(out)
			return 0, fmt.Errorf("photo %d: %w", i+1, err)
		}
		kids = append(kids, page)
	}
	pw.obj(1, "<< /Type /Catalog /Pages 2 0 R >>")
	var b bytes.Buffer
	b.WriteString("<< /Type /Pages /Kids [")
	for _, k := range kids {
		fmt.Fprintf(&b, " %d 0 R", k)
	}
	fmt.Fprintf(&b, " ] /Count %d >>", len(kids))
	pw.obj(2, b.String())
	pw.finish()
	if err := pw.err; err != nil {
		f.Close()
		os.Remove(out)
		return 0, err
	}
	if err := f.Close(); err != nil {
		os.Remove(out)
		return 0, err
	}
	return len(kids), nil
}

// pdfWriter writes numbered objects and remembers their offsets for the xref.
type pdfWriter struct {
	w    *bufio.Writer
	n    int64
	offs map[int]int64
	next int
	err  error
}

func (p *pdfWriter) Write(b []byte) (int, error) {
	if p.err != nil {
		return 0, p.err
	}
	n, err := p.w.Write(b)
	p.n += int64(n)
	p.err = err
	return n, err
}

func (p *pdfWriter) printf(format string, a ...any) { fmt.Fprintf(p, format, a...) }

func (p *pdfWriter) reserve(n int) {
	if p.offs == nil {
		p.offs = map[int]int64{}
	}
	p.next += n
}

func (p *pdfWriter) alloc() int { p.next++; return p.next }

func (p *pdfWriter) begin(id int) {
	p.offs[id] = p.n
	p.printf("%d 0 obj\n", id)
}

func (p *pdfWriter) obj(id int, body string) {
	p.begin(id)
	p.printf("%s\nendobj\n", body)
}

// stream writes object id as a stream with dict entries, its body from fill,
// and the length as a separate object so the body can be streamed.
func (p *pdfWriter) stream(id int, dict string, fill func(w io.Writer) error) {
	lenID := p.alloc()
	p.begin(id)
	p.printf("<< %s /Length %d 0 R >>\nstream\n", dict, lenID)
	start := p.n
	if err := fill(p); err != nil && p.err == nil {
		p.err = err
	}
	length := p.n - start
	p.printf("\nendstream\nendobj\n")
	p.obj(lenID, strconv.FormatInt(length, 10))
}

func (p *pdfWriter) finish() {
	xref := p.n
	p.printf("xref\n0 %d\n0000000000 65535 f \n", p.next+1)
	for i := 1; i <= p.next; i++ {
		p.printf("%010d 00000 n \n", p.offs[i])
	}
	p.printf("trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", p.next+1, xref)
	if err := p.w.Flush(); err != nil && p.err == nil {
		p.err = err
	}
}

// addPhoto writes one page holding the photo at path and returns the page's
// object number. With preview set it also writes a small PNG to prevPath.
func (p *pdfWriter) addPhoto(path string, preview bool, prevPath string) (int, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return 0, fmt.Errorf("not a photo Printty can read: %w", err)
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 30000 || cfg.Height > 30000 || cfg.Width*cfg.Height > 120_000_000 {
		return 0, errors.New("the photo is too large")
	}
	imgID := p.alloc()
	passthrough := format == "jpeg" && (cfg.ColorModel == color.YCbCrModel || cfg.ColorModel == color.GrayModel)
	var img image.Image
	if !passthrough || preview {
		img, _, err = image.Decode(bytes.NewReader(raw))
		if err != nil {
			return 0, fmt.Errorf("decode: %w", err)
		}
	}
	if passthrough {
		cs := "/DeviceRGB"
		if cfg.ColorModel == color.GrayModel {
			cs = "/DeviceGray"
		}
		p.stream(imgID, fmt.Sprintf("/Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace %s /BitsPerComponent 8 /Filter /DCTDecode", cfg.Width, cfg.Height, cs),
			func(w io.Writer) error { _, err := w.Write(raw); return err })
	} else {
		raw = nil
		p.flateImage(imgID, img)
	}
	if preview {
		b := img.Bounds()
		scale := float64(thumbSize) / maxf(float64(b.Dx()), float64(b.Dy()))
		var th image.Image = img
		if scale < 1 {
			th = shrink(img, max(1, int(float64(b.Dx())*scale+0.5)), max(1, int(float64(b.Dy())*scale+0.5)))
		}
		if err := writePNG(prevPath, th); err != nil {
			return 0, err
		}
	}
	img = nil

	// Page size: the photo at photoDPI, kept within PDF's 200-inch limit.
	wPt, hPt := float64(cfg.Width)*72/photoDPI, float64(cfg.Height)*72/photoDPI
	if m := maxf(wPt, hPt); m > 14400 {
		wPt, hPt = wPt*14400/m, hPt*14400/m
	}
	wPt, hPt = maxf(wPt, 3), maxf(hPt, 3)
	content := fmt.Sprintf("q %.3f 0 0 %.3f 0 0 cm /Im0 Do Q", wPt, hPt)
	contID := p.alloc()
	p.stream(contID, "", func(w io.Writer) error { _, err := io.WriteString(w, content); return err })
	pageID := p.alloc()
	p.obj(pageID, fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %.3f %.3f] /Resources << /XObject << /Im0 %d 0 R >> >> /Contents %d 0 R >>", wPt, hPt, imgID, contID))
	return pageID, p.err
}

// flateImage stores img losslessly: grey or RGB, with a soft mask when it
// has transparency.
func (p *pdfWriter) flateImage(id int, img image.Image) {
	b := img.Bounds()
	gray := false
	switch img.(type) {
	case *image.Gray, *image.Gray16:
		gray = true
	}
	opaque := true
	if o, ok := img.(interface{ Opaque() bool }); ok {
		opaque = o.Opaque()
	}
	maskID := 0
	if !opaque {
		maskID = p.alloc()
	}
	cs, comps := "/DeviceRGB", 3
	if gray {
		cs, comps = "/DeviceGray", 1
	}
	dict := fmt.Sprintf("/Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace %s /BitsPerComponent 8 /Filter /FlateDecode", b.Dx(), b.Dy(), cs)
	if maskID != 0 {
		dict += fmt.Sprintf(" /SMask %d 0 R", maskID)
	}
	p.stream(id, dict, func(w io.Writer) error {
		return deflateRows(w, b, comps, func(row []byte, y int) {
			for x := b.Min.X; x < b.Max.X; x++ {
				i := (x - b.Min.X) * comps
				c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
				if gray {
					row[i] = c.R
				} else {
					row[i], row[i+1], row[i+2] = c.R, c.G, c.B
				}
			}
		})
	})
	if maskID != 0 {
		p.stream(maskID, fmt.Sprintf("/Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceGray /BitsPerComponent 8 /Filter /FlateDecode", b.Dx(), b.Dy()),
			func(w io.Writer) error {
				return deflateRows(w, b, 1, func(row []byte, y int) {
					for x := b.Min.X; x < b.Max.X; x++ {
						_, _, _, a := img.At(x, y).RGBA()
						row[x-b.Min.X] = byte(a >> 8)
					}
				})
			})
	}
}

func deflateRows(w io.Writer, b image.Rectangle, comps int, fill func(row []byte, y int)) error {
	zw, err := zlib.NewWriterLevel(w, zlib.BestSpeed)
	if err != nil {
		return err
	}
	row := make([]byte, b.Dx()*comps)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		fill(row, y)
		if _, err := zw.Write(row); err != nil {
			return err
		}
	}
	return zw.Close()
}
