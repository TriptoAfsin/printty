// Package memtrim hands freed memory back to the OS after a print job, so
// Printty's idle footprint stays small between jobs.
package memtrim

import (
	"runtime"
	"runtime/debug"
)

// Now runs the GC, returns freed heap to the OS and trims the working set.
func Now() {
	runtime.GC()
	debug.FreeOSMemory()
	trimWorkingSet()
}
