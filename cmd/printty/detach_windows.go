package main

import (
	"os/exec"
	"syscall"
)

// startDetached starts a copy of Printty that outlives this one, with no window.
func startDetached(exe string, args ...string) error {
	cmd := exec.Command(exe, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000 | 0x00000008} // no window, detached
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
