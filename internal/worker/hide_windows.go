//go:build windows

package worker

import (
	"os/exec"
	"syscall"
)

// hideWindow stops a console window flashing up for each print job.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}
