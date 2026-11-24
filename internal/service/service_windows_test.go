//go:build windows

package service

import "testing"

// The status check must work without admin rights; the Print Spooler is
// always installed and running on Windows.
func TestStatusWithoutAdmin(t *testing.T) {
	st := statusOf("Spooler")
	if !st.Installed || !st.Running {
		t.Fatalf("Spooler status = %+v, want installed and running (admin=%v)", st, IsAdmin())
	}
	if statusOf("NoSuchServicePrintty").Installed {
		t.Fatal("missing service reported as installed")
	}
}
