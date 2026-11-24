//go:build !windows

package server

import "syscall"

func detached() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setsid: true} }
