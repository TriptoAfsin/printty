//go:build windows

package printer

import (
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
