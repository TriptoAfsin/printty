package printer

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

// writePhotos writes a landscape JPEG, a portrait grey JPEG and a PNG with
// transparency, and returns their paths in that order.
func writePhotos(t *testing.T, dir string) []string {
	t.Helper()
	rgb := image.NewRGBA(image.Rect(0, 0, 600, 400))
	for y := 0; y < 400; y++ {
		for x := 0; x < 600; x++ {
			rgb.Set(x, y, color.RGBA{uint8(x), uint8(y), 120, 255})
		}
	}
	gray := image.NewGray(image.Rect(0, 0, 300, 500))
	for i := range gray.Pix {
		gray.Pix[i] = uint8(i)
	}
	alpha := image.NewNRGBA(image.Rect(0, 0, 200, 200))
	for y := 0; y < 200; y++ {
		for x := 0; x < 200; x++ {
			alpha.Set(x, y, color.NRGBA{200, 30, 30, uint8(x)})
		}
	}
	var paths []string
	for i, enc := range []func(*bytes.Buffer) error{
		func(b *bytes.Buffer) error { return jpeg.Encode(b, rgb, nil) },
		func(b *bytes.Buffer) error { return jpeg.Encode(b, gray, nil) },
		func(b *bytes.Buffer) error { return png.Encode(b, alpha) },
	} {
		var b bytes.Buffer
		if err := enc(&b); err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dir, []string{"a.jpg", "b.jpg", "c.png"}[i])
		if err := os.WriteFile(p, b.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	return paths
}

func TestComposePhotos(t *testing.T) {
	dir := t.TempDir()
	paths := writePhotos(t, dir)
	out := filepath.Join(dir, "album.pdf")
	prev := filepath.Join(dir, "prev")
	n, err := ComposePhotos(context.Background(), paths, out, prev, 2)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("pages = %d, want 3", n)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"%PDF-1.4", "/Count 3", "/DCTDecode", "/FlateDecode", "/SMask", "/DeviceGray", "%%EOF",
		"/MediaBox [0 0 144.000 96.000]", "/MediaBox [0 0 72.000 120.000]"} {
		if !bytes.Contains(b, []byte(want)) {
			t.Errorf("PDF lacks %q", want)
		}
	}
	// Every xref offset points at its object.
	xref := regexp.MustCompile(`(?s)xref\n0 (\d+)\n(.*?)trailer`).FindSubmatch(b)
	if xref == nil {
		t.Fatal("no xref")
	}
	for i, line := range regexp.MustCompile(`(\d{10}) 00000 n `).FindAllSubmatch(xref[2], -1) {
		var off int
		for _, c := range line[1] {
			off = off*10 + int(c-'0')
		}
		want := []byte(strconv.Itoa(i+1) + " 0 obj")
		if !bytes.HasPrefix(b[off:], want) {
			t.Errorf("object %d: offset %d doesn't start %q", i+1, off, want)
		}
	}
	// Previews for the first two only.
	for i, want := range []bool{true, true, false} {
		_, err := os.Stat(filepath.Join(prev, strconv.Itoa(i+1)+".png"))
		if (err == nil) != want {
			t.Errorf("preview %d exists = %v, want %v", i+1, err == nil, want)
		}
	}
	// The JPEG went in untouched.
	orig, _ := os.ReadFile(paths[0])
	if !bytes.Contains(b, orig) {
		t.Error("JPEG was re-encoded")
	}
}

func TestComposePhotosRejects(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.jpg")
	os.WriteFile(bad, []byte("\xff\xd8\xffnot really"), 0o600)
	out := filepath.Join(dir, "out.pdf")
	if _, err := ComposePhotos(context.Background(), []string{bad}, out, filepath.Join(dir, "p"), 5); err == nil {
		t.Fatal("damaged photo accepted")
	}
	if _, err := os.Stat(out); err == nil {
		t.Error("half-written PDF left behind")
	}
	if _, err := ComposePhotos(context.Background(), nil, out, dir, 5); err == nil {
		t.Error("no photos accepted")
	}
}
