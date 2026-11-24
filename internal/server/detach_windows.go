package server

import "syscall"

// detached starts a child that outlives this process and has no console window.
func detached() *syscall.SysProcAttr {
	const createNoWindow, detachedProcess = 0x08000000, 0x00000008
	return &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow | detachedProcess}
}
