//go:build windows

package printer

import (
	"context"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsPDFRender(t *testing.T) {
	path := writeTestPDF(t, 2)
	err := withWinRT(func() error {
		doc, err := openPDF(path)
		if err != nil {
			return err
		}
		defer doc.close()
		if doc.pages != 2 {
			t.Fatalf("pages = %d, want 2", doc.pages)
		}
		w, h, err := doc.pageSize(0)
		if err != nil {
			return err
		}
		t.Logf("page 1 size: %.1f x %.1f DIPs", w, h)
		img, err := doc.render(1, 620, 877)
		if err != nil {
			return err
		}
		b := img.Bounds()
		if b.Dx() < 600 || b.Dx() > 640 || b.Dy() < 857 || b.Dy() > 897 {
			t.Errorf("render size %v", b)
		}
		if out := os.Getenv("PRINTTY_RENDER_OUT"); out != "" {
			f, _ := os.Create(filepath.Join(out, "page2.png"))
			png.Encode(f, img)
			f.Close()
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Photos combined into a PDF open in Windows' own PDF engine, with the
// page shapes of the photos.
func TestWindowsComposedPhotos(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "album.pdf")
	if _, err := ComposePhotos(context.Background(), writePhotos(t, dir), out, filepath.Join(dir, "p"), 0); err != nil {
		t.Fatal(err)
	}
	err := withWinRT(func() error {
		doc, err := openPDF(out)
		if err != nil {
			return err
		}
		defer doc.close()
		if doc.pages != 3 {
			t.Fatalf("pages = %d, want 3", doc.pages)
		}
		for i, want := range [][2]float32{{192, 128}, {96, 160}, {64, 64}} { // DIPs: px/300*96
			w, h, err := doc.pageSize(i)
			if err != nil {
				return err
			}
			if w < want[0]-1 || w > want[0]+1 || h < want[1]-1 || h > want[1]+1 {
				t.Errorf("page %d size %.1fx%.1f, want %v", i+1, w, h, want)
			}
		}
		img, err := doc.render(0, 600, 400)
		if err != nil {
			return err
		}
		// The test photo's red is x and its green y, both wrapping at 256.
		r, g, b, _ := img.At(590, 390).RGBA()
		if d := int(r>>8) - 590%256; d < -12 || d > 12 {
			t.Errorf("red = %d, want about %d", r>>8, 590%256)
		}
		if d := int(g>>8) - 390%256; d < -12 || d > 12 {
			t.Errorf("green = %d, want about %d", g>>8, 390%256)
		}
		if d := int(b>>8) - 120; d < -12 || d > 12 {
			t.Errorf("blue = %d, want about 120", b>>8)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
