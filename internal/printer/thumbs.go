package printer

import (
	"image"
	"image/png"
	"os"
)

// Page previews, shared by every backend.

// thumbSize is the longest side of a page preview, in pixels.
const thumbSize = 240

func maxf(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func writePNG(path string, img image.Image) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(f, img); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// shrink scales img down to w×h by averaging.
func shrink(img image.Image, w, h int) image.Image {
	b := img.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		y0, y1 := b.Min.Y+y*b.Dy()/h, b.Min.Y+(y+1)*b.Dy()/h
		for x := 0; x < w; x++ {
			x0, x1 := b.Min.X+x*b.Dx()/w, b.Min.X+(x+1)*b.Dx()/w
			var r, g, bl, a, n uint64
			for sy := y0; sy < max(y1, y0+1); sy += max(1, (y1-y0)/4) {
				for sx := x0; sx < max(x1, x0+1); sx += max(1, (x1-x0)/4) {
					cr, cg, cb, ca := img.At(sx, sy).RGBA()
					r, g, bl, a, n = r+uint64(cr), g+uint64(cg), bl+uint64(cb), a+uint64(ca), n+1
				}
			}
			if a == 0 {
				continue
			}
			i := y*out.Stride + x*4
			out.Pix[i] = uint8(r * 0xff / a)
			out.Pix[i+1] = uint8(g * 0xff / a)
			out.Pix[i+2] = uint8(bl * 0xff / a)
			out.Pix[i+3] = uint8(a / n >> 8)
		}
	}
	return out
}
