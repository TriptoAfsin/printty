// Command mkicon builds packaging/windows/printty.ico (for the installer and
// printty.exe's own icon) from the app icon PNG. Run from the repo root:
//
//	go run ./tools/mkicon
package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"log"
	"os"
)

func main() {
	f, err := os.Open("internal/web/static/icon-512.png")
	if err != nil {
		log.Fatal(err)
	}
	src, err := png.Decode(f)
	f.Close()
	if err != nil {
		log.Fatal(err)
	}
	sizes := []int{16, 20, 24, 32, 40, 48, 64, 128, 256}
	var images [][]byte
	for _, n := range sizes {
		var b bytes.Buffer
		if err := png.Encode(&b, scale(src, n)); err != nil {
			log.Fatal(err)
		}
		images = append(images, b.Bytes())
	}
	// ICO: header, one directory entry per size, then the PNGs.
	var out bytes.Buffer
	binary.Write(&out, binary.LittleEndian, [3]uint16{0, 1, uint16(len(sizes))})
	offset := 6 + 16*len(sizes)
	for i, n := range sizes {
		dim := uint8(n)
		if n >= 256 {
			dim = 0
		}
		binary.Write(&out, binary.LittleEndian, struct {
			W, H, Colors, Reserved uint8
			Planes, Bits           uint16
			Size, Offset           uint32
		}{dim, dim, 0, 0, 1, 32, uint32(len(images[i])), uint32(offset)})
		offset += len(images[i])
	}
	for _, b := range images {
		out.Write(b)
	}
	if err := os.WriteFile("packaging/windows/printty.ico", out.Bytes(), 0o644); err != nil {
		log.Fatal(err)
	}
}

// scale shrinks img to n×n by averaging (premultiplied, so edges stay clean).
func scale(img image.Image, n int) *image.NRGBA {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	out := image.NewNRGBA(image.Rect(0, 0, n, n))
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			var r, g, bl, a, c uint64
			for sy := y * h / n; sy < (y+1)*h/n; sy++ {
				for sx := x * w / n; sx < (x+1)*w/n; sx++ {
					cr, cg, cb, ca := img.At(b.Min.X+sx, b.Min.Y+sy).RGBA()
					r, g, bl, a, c = r+uint64(cr), g+uint64(cg), bl+uint64(cb), a+uint64(ca), c+1
				}
			}
			if a == 0 {
				continue
			}
			out.SetNRGBA(x, y, color.NRGBA{uint8(r * 255 / a), uint8(g * 255 / a), uint8(bl * 255 / a), uint8(a / c >> 8)})
		}
	}
	return out
}
