//go:build windows

// Package tray shows Printty's icon in the Windows notification area, with a
// menu to open the web page, copy the link for phones, and stop Printty.
// It uses plain Win32 calls (Shell_NotifyIcon), no cgo.
package tray

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const Supported = true

// Item is one menu entry. An empty Label is a separator.
type Item struct {
	Label    string
	Disabled bool
	Default  bool // bold; also what a left click does
	Do       func()
}

// Config describes the icon. State and Menu are called on the tray's thread
// whenever they're needed, so they always reflect the current state.
type Config struct {
	Icon  []byte                       // PNG, scaled down to the tray size
	State func() (tip string, on bool) // tooltip, and whether to show the icon in colour
	Menu  func() []Item
	Every time.Duration // how often to refresh State (0: never)
}

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	pRegisterClassExW       = user32.NewProc("RegisterClassExW")
	pCreateWindowExW        = user32.NewProc("CreateWindowExW")
	pDefWindowProcW         = user32.NewProc("DefWindowProcW")
	pDestroyWindow          = user32.NewProc("DestroyWindow")
	pGetMessageW            = user32.NewProc("GetMessageW")
	pTranslateMessage       = user32.NewProc("TranslateMessage")
	pDispatchMessageW       = user32.NewProc("DispatchMessageW")
	pPostMessageW           = user32.NewProc("PostMessageW")
	pPostQuitMessage        = user32.NewProc("PostQuitMessage")
	pRegisterWindowMessageW = user32.NewProc("RegisterWindowMessageW")
	pSetTimer               = user32.NewProc("SetTimer")
	pCreatePopupMenu        = user32.NewProc("CreatePopupMenu")
	pAppendMenuW            = user32.NewProc("AppendMenuW")
	pSetMenuDefaultItem     = user32.NewProc("SetMenuDefaultItem")
	pTrackPopupMenu         = user32.NewProc("TrackPopupMenu")
	pDestroyMenu            = user32.NewProc("DestroyMenu")
	pGetCursorPos           = user32.NewProc("GetCursorPos")
	pSetForegroundWindow    = user32.NewProc("SetForegroundWindow")
	pGetSystemMetrics       = user32.NewProc("GetSystemMetrics")
	pCreateIconIndirect     = user32.NewProc("CreateIconIndirect")
	pDestroyIcon            = user32.NewProc("DestroyIcon")
	pSetDpiAwarenessContext = user32.NewProc("SetProcessDpiAwarenessContext")
	pOpenClipboard          = user32.NewProc("OpenClipboard")
	pEmptyClipboard         = user32.NewProc("EmptyClipboard")
	pSetClipboardData       = user32.NewProc("SetClipboardData")
	pCloseClipboard         = user32.NewProc("CloseClipboard")
	pShellNotifyIconW       = shell32.NewProc("Shell_NotifyIconW")
	pCreateDIBSection       = gdi32.NewProc("CreateDIBSection")
	pCreateBitmap           = gdi32.NewProc("CreateBitmap")
	pDeleteObject           = gdi32.NewProc("DeleteObject")
	pGlobalAlloc            = kernel32.NewProc("GlobalAlloc")
	pGlobalLock             = kernel32.NewProc("GlobalLock")
	pGlobalUnlock           = kernel32.NewProc("GlobalUnlock")
	pGetModuleHandleW       = kernel32.NewProc("GetModuleHandleW")
	pRtlMoveMemory          = kernel32.NewProc("RtlMoveMemory")
)

const (
	wmDestroy     = 0x0002
	wmClose       = 0x0010
	wmNull        = 0x0000
	wmTimer       = 0x0113
	wmLButtonUp   = 0x0202
	wmRButtonUp   = 0x0205
	wmContextMenu = 0x007B
	wmApp         = 0x8000
	wmTray        = wmApp + 1
	wmBalloon     = wmApp + 2

	nimAdd    = 0
	nimModify = 1
	nimDelete = 2

	nifMessage = 0x01
	nifIcon    = 0x02
	nifTip     = 0x04
	nifInfo    = 0x10

	mfString    = 0x0000
	mfGrayed    = 0x0001
	mfSeparator = 0x0800

	tpmRightButton = 0x0002
	tpmBottomAlign = 0x0020
	tpmReturnCmd   = 0x0100

	smCxSmIcon = 49
)

type notifyIconData struct {
	Size            uint32
	Wnd             windows.Handle
	ID              uint32
	Flags           uint32
	CallbackMessage uint32
	Icon            windows.Handle
	Tip             [128]uint16
	State           uint32
	StateMask       uint32
	Info            [256]uint16
	Version         uint32
	InfoTitle       [64]uint16
	InfoFlags       uint32
	GUID            windows.GUID
	BalloonIcon     windows.Handle
}

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   windows.Handle
	Icon       windows.Handle
	Cursor     windows.Handle
	Background windows.Handle
	MenuName   *uint16
	ClassName  *uint16
	IconSm     windows.Handle
}

type msg struct {
	Wnd     windows.Handle
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	X, Y    int32
	Private uint32
}

// Tray is a running notification-area icon.
type Tray struct {
	cfg       Config
	wnd       windows.Handle
	created   uint32 // "TaskbarCreated": Explorer restarted, add the icon again
	added     bool
	iconOn    windows.Handle
	iconOff   windows.Handle
	tip       string
	on        bool
	items     []Item
	balloonMu sync.Mutex
	balloon   [2]string
	ready     chan struct{}
	done      chan struct{}
}

var (
	current   *Tray // one tray per process: the window procedure finds it here
	classOnce sync.Once
	wndProcCB uintptr
)

// Run shows the icon and blocks until ctx ends or a menu item calls Quit.
func Run(ctx context.Context, cfg Config) error {
	t := &Tray{cfg: cfg, ready: make(chan struct{}), done: make(chan struct{})}
	return t.run(ctx)
}

// Start shows the icon on its own thread and returns once it's up.
func Start(ctx context.Context, cfg Config) (*Tray, error) {
	t := &Tray{cfg: cfg, ready: make(chan struct{}), done: make(chan struct{})}
	errc := make(chan error, 1)
	go func() { errc <- t.run(ctx) }()
	select {
	case <-t.ready:
		return t, nil
	case err := <-errc:
		return nil, err
	}
}

func (t *Tray) run(ctx context.Context) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(t.done)
	if current != nil {
		return errors.New("tray already running")
	}
	current = t
	defer func() { current = nil }()

	// Sharp icon on high-DPI screens. Best effort: fails if already set.
	if pSetDpiAwarenessContext.Find() == nil {
		pSetDpiAwarenessContext.Call(^uintptr(3)) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 (-4)
	}
	size, _, _ := pGetSystemMetrics.Call(smCxSmIcon)
	if size == 0 {
		size = 16
	}
	var err error
	if t.iconOn, err = makeIcon(t.cfg.Icon, int(size), false); err != nil {
		return err
	}
	defer pDestroyIcon.Call(uintptr(t.iconOn))
	if t.iconOff, err = makeIcon(t.cfg.Icon, int(size), true); err != nil {
		return err
	}
	defer pDestroyIcon.Call(uintptr(t.iconOff))

	inst, _, _ := pGetModuleHandleW.Call(0)
	className := windows.StringToUTF16Ptr("PrinttyTray")
	var regErr error
	classOnce.Do(func() {
		wndProcCB = windows.NewCallback(wndProc)
		wc := wndClassEx{WndProc: wndProcCB, Instance: windows.Handle(inst), ClassName: className}
		wc.Size = uint32(unsafe.Sizeof(wc))
		if r, _, e := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
			regErr = fmt.Errorf("register tray window: %w", e)
		}
	})
	if regErr != nil {
		return regErr
	}
	// A hidden top-level window (not message-only, so it hears TaskbarCreated).
	h, _, e := pCreateWindowExW.Call(0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(windows.StringToUTF16Ptr("Printty"))),
		0, 0, 0, 0, 0, 0, 0, inst, 0)
	if h == 0 {
		return fmt.Errorf("create tray window: %w", e)
	}
	t.wnd = windows.Handle(h)
	created, _, _ := pRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(windows.StringToUTF16Ptr("TaskbarCreated"))))
	t.created = uint32(created)

	t.refresh()
	t.add()
	every := t.cfg.Every
	if every <= 0 {
		every = 10 * time.Second // also retries adding the icon if Explorer wasn't up yet
	}
	pSetTimer.Call(h, 1, uintptr(every.Milliseconds()), 0)
	close(t.ready)

	go func() {
		<-ctx.Done()
		pPostMessageW.Call(h, wmClose, 0, 0)
	}()

	var m msg
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	return nil
}

// Quit removes the icon and ends Run.
func (t *Tray) Quit() {
	if t != nil && t.wnd != 0 {
		pPostMessageW.Call(uintptr(t.wnd), wmClose, 0, 0)
	}
}

// Done is closed once the icon is gone.
func (t *Tray) Done() <-chan struct{} { return t.done }

// Single reports whether this is the only process holding name, so a second
// copy of the tray can quit. The hold lasts until the process exits.
func Single(name string) bool {
	_, err := windows.CreateMutex(nil, false, windows.StringToUTF16Ptr(`Local\`+name))
	return !errors.Is(err, windows.ERROR_ALREADY_EXISTS)
}

// Notify shows a Windows notification from the icon.
func (t *Tray) Notify(title, text string) {
	if t == nil || t.wnd == 0 {
		return
	}
	t.balloonMu.Lock()
	t.balloon = [2]string{title, text}
	t.balloonMu.Unlock()
	pPostMessageW.Call(uintptr(t.wnd), wmBalloon, 0, 0)
}

func (t *Tray) data(flags uint32) *notifyIconData {
	d := &notifyIconData{Wnd: t.wnd, ID: 1, Flags: flags, CallbackMessage: wmTray}
	d.Size = uint32(unsafe.Sizeof(*d))
	d.Icon = t.iconOff
	if t.on {
		d.Icon = t.iconOn
	}
	copyUTF16(d.Tip[:], t.tip)
	return d
}

func (t *Tray) add() {
	r, _, _ := pShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(t.data(nifMessage|nifIcon|nifTip))))
	t.added = r != 0
}

func (t *Tray) refresh() {
	if t.cfg.State == nil {
		return
	}
	tip, on := t.cfg.State()
	if tip == t.tip && on == t.on && t.added {
		return
	}
	t.tip, t.on = tip, on
	if t.added {
		pShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(t.data(nifIcon|nifTip))))
	}
}

func (t *Tray) showMenu(x, y int32) {
	if t.cfg.Menu == nil {
		return
	}
	t.items = t.cfg.Menu()
	m, _, _ := pCreatePopupMenu.Call()
	defer pDestroyMenu.Call(m)
	for i, it := range t.items {
		if it.Label == "" {
			pAppendMenuW.Call(m, mfSeparator, 0, 0)
			continue
		}
		flags := uintptr(mfString)
		if it.Disabled || it.Do == nil {
			flags |= mfGrayed
		}
		pAppendMenuW.Call(m, flags, uintptr(i+1), uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(it.Label))))
		if it.Default {
			pSetMenuDefaultItem.Call(m, uintptr(i+1), 0)
		}
	}
	// Without this the menu doesn't close when you click elsewhere.
	pSetForegroundWindow.Call(uintptr(t.wnd))
	cmd, _, _ := pTrackPopupMenu.Call(m, tpmRightButton|tpmBottomAlign|tpmReturnCmd, uintptr(x), uintptr(y), 0, uintptr(t.wnd), 0)
	pPostMessageW.Call(uintptr(t.wnd), wmNull, 0, 0)
	if cmd > 0 && int(cmd) <= len(t.items) {
		if do := t.items[cmd-1].Do; do != nil {
			go do()
		}
	}
}

func (t *Tray) defaultAction() {
	if t.cfg.Menu == nil {
		return
	}
	for _, it := range t.cfg.Menu() {
		if it.Default && it.Do != nil && !it.Disabled {
			go it.Do()
			return
		}
	}
}

func wndProc(hwnd, message, wparam, lparam uintptr) uintptr {
	t := current
	if t == nil {
		r, _, _ := pDefWindowProcW.Call(hwnd, message, wparam, lparam)
		return r
	}
	switch uint32(message) {
	case wmTray:
		switch uint32(lparam) {
		case wmLButtonUp:
			t.defaultAction()
		case wmRButtonUp, wmContextMenu:
			var pt struct{ X, Y int32 }
			pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
			t.showMenu(pt.X, pt.Y)
		}
		return 0
	case wmTimer:
		if !t.added {
			t.add()
		}
		t.refresh()
		return 0
	case wmBalloon:
		t.balloonMu.Lock()
		b := t.balloon
		t.balloonMu.Unlock()
		d := t.data(nifInfo)
		copyUTF16(d.InfoTitle[:], b[0])
		copyUTF16(d.Info[:], b[1])
		pShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(d)))
		return 0
	case wmClose:
		pDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		pShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(t.data(0))))
		pPostQuitMessage.Call(0)
		return 0
	}
	if t.created != 0 && uint32(message) == t.created {
		t.add()
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, message, wparam, lparam)
	return r
}

func copyUTF16(dst []uint16, s string) {
	u, _ := windows.UTF16FromString(s)
	if len(u) > len(dst) {
		u = u[:len(dst)]
		u[len(u)-1] = 0
	}
	copy(dst, u)
}

// makeIcon scales the PNG to size×size (area average) and builds an HICON.
// grey makes the "stopped" variant.
func makeIcon(pngData []byte, size int, grey bool) (windows.Handle, error) {
	img, err := png.Decode(bytes.NewReader(pngData))
	if err != nil {
		return 0, fmt.Errorf("tray icon: %w", err)
	}
	px := scale(img, size)
	if grey {
		for i := 0; i < len(px); i += 4 {
			b, g, r := float64(px[i]), float64(px[i+1]), float64(px[i+2])
			l := uint8(0.3*r + 0.59*g + 0.11*b)
			px[i], px[i+1], px[i+2] = l, l, l
			px[i+3] = uint8(float64(px[i+3]) * 0.7)
		}
	}
	type bitmapInfoHeader struct {
		Size                     uint32
		Width, Height            int32
		Planes, BitCount         uint16
		Compression, SizeImage   uint32
		XPelsPerMeter, YPerMeter int32
		ClrUsed, ClrImportant    uint32
	}
	bi := bitmapInfoHeader{Width: int32(size), Height: -int32(size), Planes: 1, BitCount: 32}
	bi.Size = uint32(unsafe.Sizeof(bi))
	var bits unsafe.Pointer
	color, _, e := pCreateDIBSection.Call(0, uintptr(unsafe.Pointer(&bi)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if color == 0 {
		return 0, fmt.Errorf("tray icon bitmap: %w", e)
	}
	defer pDeleteObject.Call(color)
	copy(unsafe.Slice((*byte)(bits), len(px)), px)
	mask, _, _ := pCreateBitmap.Call(uintptr(size), uintptr(size), 1, 1, 0)
	defer pDeleteObject.Call(mask)
	ii := struct {
		Icon         int32
		XHot, YHot   uint32
		Mask, Colour uintptr
	}{Icon: 1, Mask: mask, Colour: color}
	h, _, e := pCreateIconIndirect.Call(uintptr(unsafe.Pointer(&ii)))
	if h == 0 {
		return 0, fmt.Errorf("tray icon: %w", e)
	}
	return windows.Handle(h), nil
}

// scale returns size×size BGRA pixels (straight alpha) averaged from img.
func scale(img image.Image, size int) []byte {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	out := make([]byte, size*size*4)
	for y := 0; y < size; y++ {
		y0, y1 := y*h/size, (y+1)*h/size
		for x := 0; x < size; x++ {
			x0, x1 := x*w/size, (x+1)*w/size
			var r, g, bl, a, n uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					cr, cg, cb, ca := img.At(b.Min.X+sx, b.Min.Y+sy).RGBA() // premultiplied, 16-bit
					r, g, bl, a = r+uint64(cr), g+uint64(cg), bl+uint64(cb), a+uint64(ca)
					n++
				}
			}
			if n == 0 || a == 0 {
				continue
			}
			i := (y*size + x) * 4
			out[i] = uint8(bl * 255 / a)
			out[i+1] = uint8(g * 255 / a)
			out[i+2] = uint8(r * 255 / a)
			out[i+3] = uint8(a / n >> 8)
		}
	}
	return out
}

// CopyText puts text on the clipboard.
func CopyText(s string) error {
	u, err := windows.UTF16FromString(s)
	if err != nil {
		return err
	}
	if r, _, e := pOpenClipboard.Call(0); r == 0 {
		return fmt.Errorf("open clipboard: %w", e)
	}
	defer pCloseClipboard.Call()
	pEmptyClipboard.Call()
	n := uintptr(len(u) * 2)
	hmem, _, e := pGlobalAlloc.Call(0x0002, n) // GMEM_MOVEABLE
	if hmem == 0 {
		return fmt.Errorf("clipboard memory: %w", e)
	}
	p, _, _ := pGlobalLock.Call(hmem)
	pRtlMoveMemory.Call(p, uintptr(unsafe.Pointer(&u[0])), n)
	pGlobalUnlock.Call(hmem)
	if r, _, e := pSetClipboardData.Call(13, hmem); r == 0 { // CF_UNICODETEXT
		return fmt.Errorf("set clipboard: %w", e)
	}
	return nil
}

// Open opens a URL or file with its default app.
func Open(target string) error {
	return windows.ShellExecute(0, windows.StringToUTF16Ptr("open"), windows.StringToUTF16Ptr(target), nil, nil, windows.SW_SHOWNORMAL)
}
