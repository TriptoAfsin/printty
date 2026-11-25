//go:build !windows

package printer

import (
	"context"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Layout, driver options and previews for the CUPS backend. CUPS lays pages
// out itself, so the shared options map to its standard job attributes;
// turning single pages by hand isn't available through lp.

// handled are PPD options Printty already offers in its own controls.
var handled = map[string]bool{
	"PageSize": true, "PageRegion": true, "Duplex": true, "ColorModel": true,
	"number-up": true, "orientation-requested": true,
}

var optionLine = regexp.MustCompile(`^([A-Za-z0-9_.-]+)/([^:]*):\s*(.*)$`)

// driverOptions lists the printer's own options from its PPD (lpoptions -l),
// for example MediaType, InputSlot and cupsPrintQuality.
func driverOptions(ctx context.Context, name string) []DriverOption {
	out, err := run(ctx, "lpoptions", "-p", name, "-l")
	if err != nil {
		return nil
	}
	var opts []DriverOption
	for _, line := range strings.Split(out, "\n") {
		m := optionLine.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil || handled[m[1]] {
			continue
		}
		o := DriverOption{Key: m[1], Label: strings.TrimSpace(m[2])}
		for _, v := range strings.Fields(m[3]) {
			if d, ok := strings.CutPrefix(v, "*"); ok {
				v = d
				o.Default = v
			}
			o.Choices = append(o.Choices, Choice{Value: v, Label: v})
		}
		if len(o.Choices) > 1 {
			opts = append(opts, o)
		}
	}
	return opts
}

var safeValue = regexp.MustCompile(`^[A-Za-z0-9_.:+-]+$`)

// cupsNumberUp is the smallest number-up CUPS supports that holds n pages.
func cupsNumberUp(n int) int {
	for _, k := range []int{1, 2, 4, 6, 9, 16} {
		if n <= k {
			return k
		}
	}
	return 16
}

// layoutArgs maps the shared layout and driver options to lp -o arguments;
// pages is how many pages the job prints.
func layoutArgs(o Options, pages int) []string {
	var args []string
	switch o.Orientation {
	case Portrait:
		args = append(args, "-o", "orientation-requested=3")
	case Landscape:
		args = append(args, "-o", "orientation-requested=4")
	}
	switch o.Fit {
	case Fill:
		args = append(args, "-o", "print-scaling=fill")
	case Actual:
		args = append(args, "-o", "print-scaling=none")
	default:
		args = append(args, "-o", "print-scaling=fit")
	}
	n := PerSheet(o)
	if o.OneSheet {
		n = pages
	}
	if n = cupsNumberUp(n); n > 1 {
		args = append(args, "-o", "number-up="+strconv.Itoa(n))
	}
	// CUPS scales images by percent of the page; copies on one sheet,
	// dragging a photo and a custom page order aren't available through lp.
	if o.Scale > 0 && o.Scale < 100 {
		args = append(args, "-o", "scaling="+strconv.Itoa(o.Scale))
	}
	return append(args, driverArgs(o)...)
}

// driverArgs passes the printer's own options (paper type, tray...) to lp.
func driverArgs(o Options) []string {
	var args []string
	keys := make([]string, 0, len(o.Driver))
	for k := range o.Driver {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := o.Driver[k]
		if safeValue.MatchString(k) && safeValue.MatchString(v) && !handled[k] {
			args = append(args, "-o", k+"="+v)
		}
	}
	return args
}

// Inspect counts pages and writes previews: pdftoppm (poppler) for PDFs when
// it's installed, Go's decoders for photos.
func (c CUPS) Inspect(ctx context.Context, path, kind, dir string, max int) (int, error) {
	n, err := c.PageCount(ctx, path, kind)
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return n, err
	}
	if kind != "pdf" {
		f, err := os.Open(path)
		if err != nil {
			return n, err
		}
		img, _, err := image.Decode(f)
		f.Close()
		if err != nil {
			return n, fmt.Errorf("decode image: %w", err)
		}
		b := img.Bounds()
		scale := float64(thumbSize) / maxf(float64(b.Dx()), float64(b.Dy()))
		return n, writePNG(filepath.Join(dir, "1.png"), shrink(img, max1(int(float64(b.Dx())*scale)), max1(int(float64(b.Dy())*scale))))
	}
	last := min(n, max)
	if _, err := run(ctx, "pdftoppm", "-png", "-scale-to", strconv.Itoa(thumbSize), "-f", "1", "-l", strconv.Itoa(last), path, filepath.Join(dir, "p")); err != nil {
		return n, nil // no previews without poppler; printing still works
	}
	// pdftoppm names files p-1.png or p-01.png depending on the page count.
	files, _ := filepath.Glob(filepath.Join(dir, "p-*.png"))
	for _, f := range files {
		num := strings.TrimLeft(strings.TrimSuffix(strings.TrimPrefix(filepath.Base(f), "p-"), ".png"), "0")
		if _, err := strconv.Atoi(num); err == nil {
			os.Rename(f, filepath.Join(dir, num+".png"))
		}
	}
	return n, nil
}

func max1(n int) int {
	if n < 1 {
		return 1
	}
	return n
}
