//go:build !windows

// Package service: on Linux and macOS, run Printty under systemd or launchd
// (see README). Windows-only features report unsupported here.
package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const (
	Name      = "Printty"
	Supported = false
)

var (
	ErrCancelled   = errors.New("cancelled")
	errUnsupported = errors.New("use systemd or launchd on this OS (see README)")
)

type State struct{ Installed, Running, AtSignIn bool }

func IsService() bool                           { return false }
func Status() State                             { return State{} }
func Install(string, []string, string) error    { return errUnsupported }
func Uninstall() error                          { return errUnsupported }
func RemoveSelf() error                         { return errUnsupported }
func Start() error                              { return errUnsupported }
func Stop() error                               { return errUnsupported }
func Run(func(ctx context.Context) error) error { return errUnsupported }
func EnableAutostart(string) error              { return errUnsupported }
func DisableAutostart() error                   { return errUnsupported }
func AutostartEnabled() bool                    { return false }
func Quote(a []string) string                   { return strings.Join(a, " ") }
func Elevate(string, []string) error            { return errUnsupported }
func IsAdmin() bool                             { return os.Geteuid() == 0 }
func HideConsole()                              {}
func ExeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(exe)
}
