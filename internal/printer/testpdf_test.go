package printer

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeTestPDF writes a minimal valid PDF with n A4 pages, each showing its number.
func writeTestPDF(t *testing.T, n int) string {
	t.Helper()
	var b bytes.Buffer
	offsets := []int{}
	obj := func(s string) {
		offsets = append(offsets, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", len(offsets), s)
	}
	b.WriteString("%PDF-1.4\n")
	// 1: catalog, 2: pages, 3: font, then page+content pairs
	kids := ""
	for i := 0; i < n; i++ {
		kids += fmt.Sprintf("%d 0 R ", 4+i*2)
	}
	obj("<< /Type /Catalog /Pages 2 0 R >>")
	obj(fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", kids, n))
	obj("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	for i := 0; i < n; i++ {
		obj(fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 3 0 R >> >> /Contents %d 0 R >>", 5+i*2))
		content := fmt.Sprintf("BT /F1 96 Tf 200 400 Td (%d) Tj ET 0.17 0.48 0.46 rg 40 40 515 30 re f", i+1)
		obj(fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content))
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(offsets)+1)
	for _, o := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets)+1, xref)
	p := filepath.Join(t.TempDir(), "test.pdf")
	if err := os.WriteFile(p, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestParsePages(t *testing.T) {
	cases := []struct {
		spec string
		n    int
		want string
	}{
		{"", 3, "[1 2 3]"},
		{"1-2,5", 6, "[1 2 5]"},
		{"2-9", 4, "[2 3 4]"},
		{"3,1,3", 4, "[3 1]"},
	}
	for _, c := range cases {
		got, err := ParsePages(c.spec, c.n)
		if err != nil || fmt.Sprint(got) != c.want {
			t.Errorf("ParsePages(%q,%d) = %v, %v; want %s", c.spec, c.n, got, err, c.want)
		}
	}
	if _, err := ParsePages("5-2", 9); err == nil {
		t.Error("expected error for reversed range")
	}
	f, bk := SplitDuplex([]int{1, 2, 3, 4, 5}, true)
	if fmt.Sprint(f) != "[1 3 5]" || fmt.Sprint(bk) != "[4 2]" {
		t.Errorf("SplitDuplex = %v %v", f, bk)
	}
}

func sleepMs(ms int) { time.Sleep(time.Duration(ms) * time.Millisecond) }
