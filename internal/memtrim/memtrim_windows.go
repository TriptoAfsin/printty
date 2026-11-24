//go:build windows

package memtrim

import "golang.org/x/sys/windows"

var procEmptyWorkingSet = windows.NewLazySystemDLL("psapi.dll").NewProc("EmptyWorkingSet")

// trimWorkingSet asks Windows to page out what Printty isn't using (DLL pages
// loaded for printing, freed buffers). Pages come back on demand.
func trimWorkingSet() {
	if procEmptyWorkingSet.Find() == nil {
		procEmptyWorkingSet.Call(uintptr(windows.CurrentProcess()))
	}
}
