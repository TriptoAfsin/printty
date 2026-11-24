//go:build windows

package tray

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/TriptoAfsin/printty/internal/web"
)

// Opt-in: it replaces what's on the clipboard, and CI machines may have none.
func TestCopyText(t *testing.T) {
	if os.Getenv("PRINTTY_CLIPBOARD_TEST") != "1" {
		t.Skip("set PRINTTY_CLIPBOARD_TEST=1 to run")
	}
	if err := CopyText("http://192.0.2.1:8631"); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("powershell", "-NoProfile", "-Command", "Get-Clipboard").Output()
	if err != nil {
		t.Skip("no PowerShell:", err)
	}
	if got := strings.TrimSpace(string(out)); got != "http://192.0.2.1:8631" {
		t.Fatalf("clipboard = %q", got)
	}
}

func TestMakeIcon(t *testing.T) {
	b, err := web.Static.ReadFile("static/icon-192.png")
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range []int{16, 24, 32} {
		for _, grey := range []bool{false, true} {
			h, err := makeIcon(b, size, grey)
			if err != nil || h == 0 {
				t.Fatalf("size %d grey %v: %v", size, grey, err)
			}
			pDestroyIcon.Call(uintptr(h))
		}
	}
}
