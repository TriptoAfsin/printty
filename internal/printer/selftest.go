package printer

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
)

// TestPDF returns a minimal valid PDF with n A4 pages, each showing its number.
func TestPDF(n int) []byte {
	sizes := make([][2]int, n)
	for i := range sizes {
		sizes[i] = [2]int{595, 842}
	}
	return TestPDFSizes(sizes)
}

// TestPDFSizes returns a PDF with one page per size (in points), each showing
// its number large, an arrow pointing up and a bar along its bottom edge, so
// rotation and placement are easy to see in a printout.
func TestPDFSizes(sizes [][2]int) []byte {
	var b bytes.Buffer
	var offsets []int
	obj := func(s string) {
		offsets = append(offsets, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", len(offsets), s)
	}
	n := len(sizes)
	b.WriteString("%PDF-1.4\n")
	kids := ""
	for i := 0; i < n; i++ {
		kids += fmt.Sprintf("%d 0 R ", 4+i*2)
	}
	obj("<< /Type /Catalog /Pages 2 0 R >>")
	obj(fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", kids, n))
	obj("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	for i, sz := range sizes {
		w, h := sz[0], sz[1]
		obj(fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %d %d] /Resources << /Font << /F1 3 0 R >> >> /Contents %d 0 R >>", w, h, 5+i*2))
		cx, cy := w/2, h/2
		content := fmt.Sprintf("0.85 0.88 0.9 rg 20 20 %d %d re f "+ // page tint
			"0.17 0.48 0.46 rg 20 20 %d 30 re f "+ // bottom bar
			"0 0 0 rg BT /F1 120 Tf %d %d Td (%d) Tj ET "+ // number
			"0.8 0.2 0.2 rg %d %d m %d %d l %d %d l f", // arrow pointing up
			w-40, h-40, w-40, cx-35, cy-60, i+1, cx-30, h-140, cx+30, h-140, cx, h-60)
		obj(fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content))
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(offsets)+1)
	for _, o := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets)+1, xref)
	return b.Bytes()
}

// SelfTest checks the PDF engine end to end without printing: it writes a
// 2-page PDF, counts its pages and renders one.
func SelfTest() (string, error) {
	dir, err := os.MkdirTemp("", "printty-selftest")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	p := filepath.Join(dir, "test.pdf")
	if err := os.WriteFile(p, TestPDF(2), 0o600); err != nil {
		return "", err
	}
	return selfTest(p)
}
