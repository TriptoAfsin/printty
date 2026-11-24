//go:build !windows

// Package tray: the notification-area icon is Windows-only for now.
package tray

import (
	"context"
	"errors"
	"time"
)

const Supported = false

var errUnsupported = errors.New("tray icon is only available on Windows")

type Item struct {
	Label    string
	Disabled bool
	Default  bool
	Do       func()
}

type Config struct {
	Icon          []byte
	State         func() (tip string, on bool)
	Menu          func() []Item
	Every         time.Duration
	OnNotifyClick func()
}

type Tray struct{}

func Run(context.Context, Config) error            { return errUnsupported }
func Start(context.Context, Config) (*Tray, error) { return nil, errUnsupported }
func (*Tray) Quit()                                {}
func (*Tray) Notify(string, string)                {}
func (*Tray) Done() <-chan struct{}                { c := make(chan struct{}); close(c); return c }
func Single(string) bool                           { return true }
func CopyText(string) error                        { return errUnsupported }
func Open(string) error                            { return errUnsupported }
