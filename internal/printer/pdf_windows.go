//go:build windows

package printer

// PDF rendering with Windows.Data.Pdf (WinRT, built into Windows 10/11).
// No cgo and no bundled PDF engine: COM vtables are called directly.

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modCombase = windows.NewLazySystemDLL("combase.dll")
	modShcore  = windows.NewLazySystemDLL("shcore.dll")
	modShlwapi = windows.NewLazySystemDLL("shlwapi.dll")

	procRoInitialize           = modCombase.NewProc("RoInitialize")
	procRoUninitialize         = modCombase.NewProc("RoUninitialize")
	procRoGetActivationFactory = modCombase.NewProc("RoGetActivationFactory")
	procRoActivateInstance     = modCombase.NewProc("RoActivateInstance")
	procWindowsCreateString    = modCombase.NewProc("WindowsCreateString")
	procWindowsDeleteString    = modCombase.NewProc("WindowsDeleteString")

	procCreateRandomAccessStreamOverStream = modShcore.NewProc("CreateRandomAccessStreamOverStream")
	procCreateStreamOverRandomAccessStream = modShcore.NewProc("CreateStreamOverRandomAccessStream")
	procSHCreateStreamOnFileEx             = modShlwapi.NewProc("SHCreateStreamOnFileEx")
)

var (
	iidIAsyncInfo            = windows.GUID{Data1: 0x00000036, Data2: 0x0000, Data3: 0x0000, Data4: [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidIStream               = windows.GUID{Data1: 0x0000000C, Data2: 0x0000, Data3: 0x0000, Data4: [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidIRandomAccessStream   = windows.GUID{Data1: 0x905A0FE1, Data2: 0xBC53, Data3: 0x11DF, Data4: [8]byte{0x8C, 0x49, 0x00, 0x1E, 0x4F, 0xC6, 0x86, 0xDA}}
	iidIPdfDocumentStatics   = windows.GUID{Data1: 0x433A0B5F, Data2: 0xC007, Data3: 0x4788, Data4: [8]byte{0x90, 0xF2, 0x08, 0x14, 0x3D, 0x92, 0x25, 0x99}}
	iidIPdfPageRenderOptions = windows.GUID{Data1: 0x3C98056F, Data2: 0xB7CF, Data3: 0x4C29, Data4: [8]byte{0x9A, 0x04, 0x52, 0xD9, 0x02, 0x67, 0xF4, 0x25}}
)

const (
	roInitMultithreaded = 1
	stgmRead            = 0x0
	stgmShareDenyWrite  = 0x20
	asyncStarted        = 0
	asyncCompleted      = 1
)

// comObj is a COM/WinRT interface pointer: an object whose first word is its vtable.
type comObj = *comIface

type comIface struct{ vt *[64]uintptr }

func (o *comIface) ptr() uintptr { return uintptr(unsafe.Pointer(o)) }

func (o *comIface) vtbl(i int) uintptr { return o.vt[i] }

func (o *comIface) call(i int, args ...uintptr) error {
	r, _, _ := syscall.SyscallN(o.vtbl(i), append([]uintptr{o.ptr()}, args...)...)
	if int32(r) < 0 {
		return fmt.Errorf("HRESULT 0x%08X", uint32(r))
	}
	return nil
}

func (o *comIface) query(iid *windows.GUID) (comObj, error) {
	var out comObj
	if err := o.call(0, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&out))); err != nil {
		return nil, fmt.Errorf("QueryInterface: %w", err)
	}
	return out, nil
}

func (o *comIface) release() {
	if o != nil {
		syscall.SyscallN(o.vtbl(2), o.ptr())
	}
}

func hresult(r uintptr, what string) error {
	if int32(r) < 0 {
		return fmt.Errorf("%s: HRESULT 0x%08X", what, uint32(r))
	}
	return nil
}

type hstring uintptr

func newHString(s string) (hstring, error) {
	u, err := windows.UTF16FromString(s)
	if err != nil {
		return 0, err
	}
	var h hstring
	r, _, _ := procWindowsCreateString.Call(uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), uintptr(unsafe.Pointer(&h)))
	return h, hresult(r, "WindowsCreateString")
}

func (h hstring) free() { procWindowsDeleteString.Call(uintptr(h)) }

func activationFactory(class string, iid *windows.GUID) (comObj, error) {
	h, err := newHString(class)
	if err != nil {
		return nil, err
	}
	defer h.free()
	var f comObj
	r, _, _ := procRoGetActivationFactory.Call(uintptr(h), uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&f)))
	return f, hresult(r, "RoGetActivationFactory "+class)
}

func activate(class string) (comObj, error) {
	h, err := newHString(class)
	if err != nil {
		return nil, err
	}
	defer h.free()
	var o comObj
	r, _, _ := procRoActivateInstance.Call(uintptr(h), uintptr(unsafe.Pointer(&o)))
	return o, hresult(r, "RoActivateInstance "+class)
}

// await polls an IAsyncOperation/IAsyncAction until it completes.
func await(op comObj, timeout time.Duration) error {
	info, err := op.query(&iidIAsyncInfo)
	if err != nil {
		return err
	}
	defer info.release()
	deadline := time.Now().Add(timeout)
	for {
		var status int32
		if err := info.call(7, uintptr(unsafe.Pointer(&status))); err != nil {
			return fmt.Errorf("IAsyncInfo.Status: %w", err)
		}
		switch status {
		case asyncStarted:
			if time.Now().After(deadline) {
				info.call(9) // Cancel
				return fmt.Errorf("WinRT operation timed out")
			}
			time.Sleep(5 * time.Millisecond)
			continue
		case asyncCompleted:
			return nil
		default:
			var code int32
			info.call(8, uintptr(unsafe.Pointer(&code)))
			return fmt.Errorf("WinRT operation failed: status %d, HRESULT 0x%08X", status, uint32(code))
		}
	}
}

// dipScale is how much WinRT enlarges DestinationWidth/Height (the display's
// scale factor, e.g. 1.5 at 150%). Measured once with a tiny render, since a
// DPI-unaware process can't ask the system for it reliably.
var dipScale float64

func (d *pdfDoc) ensureScale(index int) {
	if dipScale != 0 {
		return
	}
	dipScale = 1
	img, err := d.renderRaw(index, 64, 64)
	if err == nil {
		if s := float64(img.Bounds().Dx()) / 64; s > 0.5 && s < 4 {
			dipScale = s
		}
	}
}

// pdfDoc is an open Windows.Data.Pdf.PdfDocument.
type pdfDoc struct {
	doc    comObj
	stream comObj // IRandomAccessStream over the file
	pages  int
}

// withWinRT runs fn on a locked OS thread with WinRT initialised.
func withWinRT(fn func() error) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	r, _, _ := procRoInitialize.Call(roInitMultithreaded)
	// S_OK, S_FALSE (already initialised) are fine; RPC_E_CHANGED_MODE is not.
	if int32(r) < 0 {
		return hresult(r, "RoInitialize")
	}
	defer procRoUninitialize.Call()
	return fn()
}

func openPDF(path string) (*pdfDoc, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	var istream comObj
	r, _, _ := procSHCreateStreamOnFileEx.Call(uintptr(unsafe.Pointer(p)), stgmRead|stgmShareDenyWrite, 0, 0, 0, uintptr(unsafe.Pointer(&istream)))
	if err := hresult(r, "SHCreateStreamOnFileEx"); err != nil {
		return nil, err
	}
	defer istream.release()
	var ras comObj
	r, _, _ = procCreateRandomAccessStreamOverStream.Call(istream.ptr(), 0, uintptr(unsafe.Pointer(&iidIRandomAccessStream)), uintptr(unsafe.Pointer(&ras)))
	if err := hresult(r, "CreateRandomAccessStreamOverStream"); err != nil {
		return nil, err
	}
	statics, err := activationFactory("Windows.Data.Pdf.PdfDocument", &iidIPdfDocumentStatics)
	if err != nil {
		ras.release()
		return nil, err
	}
	defer statics.release()
	var op comObj
	// IPdfDocumentStatics.LoadFromStreamAsync (vtable 8)
	if err := statics.call(8, ras.ptr(), uintptr(unsafe.Pointer(&op))); err != nil {
		ras.release()
		return nil, fmt.Errorf("LoadFromStreamAsync: %w", err)
	}
	defer op.release()
	if err := await(op, 60*time.Second); err != nil {
		ras.release()
		return nil, fmt.Errorf("open PDF: %w", err)
	}
	var doc comObj
	if err := op.call(8, uintptr(unsafe.Pointer(&doc))); err != nil { // GetResults
		ras.release()
		return nil, fmt.Errorf("open PDF results: %w", err)
	}
	var n uint32
	if err := doc.call(7, uintptr(unsafe.Pointer(&n))); err != nil { // get_PageCount
		doc.release()
		ras.release()
		return nil, fmt.Errorf("PageCount: %w", err)
	}
	return &pdfDoc{doc: doc, stream: ras, pages: int(n)}, nil
}

func (d *pdfDoc) close() {
	d.doc.release()
	d.stream.release()
}

// pageSize returns the page size in DIPs (1/96 inch).
func (d *pdfDoc) pageSize(index int) (w, h float32, err error) {
	var page comObj
	if err := d.doc.call(6, uintptr(index), uintptr(unsafe.Pointer(&page))); err != nil {
		return 0, 0, fmt.Errorf("GetPage %d: %w", index, err)
	}
	defer page.release()
	var size struct{ W, H float32 }
	if err := page.call(10, uintptr(unsafe.Pointer(&size))); err != nil { // get_Size
		return 0, 0, fmt.Errorf("page size: %w", err)
	}
	return size.W, size.H, nil
}

// render draws page index (0-based) at widthPx × heightPx and returns the image.
func (d *pdfDoc) render(index, widthPx, heightPx int) (image.Image, error) {
	d.ensureScale(index)
	if dipScale > 1.01 {
		widthPx = int(float64(widthPx)/dipScale + 0.5)
		heightPx = int(float64(heightPx)/dipScale + 0.5)
	}
	return d.renderRaw(index, widthPx, heightPx)
}

func (d *pdfDoc) renderRaw(index, widthPx, heightPx int) (image.Image, error) {
	var page comObj
	if err := d.doc.call(6, uintptr(index), uintptr(unsafe.Pointer(&page))); err != nil {
		return nil, fmt.Errorf("GetPage %d: %w", index, err)
	}
	defer page.release()

	optsInsp, err := activate("Windows.Data.Pdf.PdfPageRenderOptions")
	if err != nil {
		return nil, err
	}
	defer optsInsp.release()
	opts, err := optsInsp.query(&iidIPdfPageRenderOptions)
	if err != nil {
		return nil, err
	}
	defer opts.release()
	if err := opts.call(9, uintptr(uint32(widthPx))); err != nil { // put_DestinationWidth
		return nil, fmt.Errorf("DestinationWidth: %w", err)
	}
	if err := opts.call(11, uintptr(uint32(heightPx))); err != nil { // put_DestinationHeight
		return nil, fmt.Errorf("DestinationHeight: %w", err)
	}

	memInsp, err := activate("Windows.Storage.Streams.InMemoryRandomAccessStream")
	if err != nil {
		return nil, err
	}
	defer memInsp.release()
	mem, err := memInsp.query(&iidIRandomAccessStream)
	if err != nil {
		return nil, err
	}
	defer mem.release()

	var action comObj
	// IPdfPage.RenderWithOptionsToStreamAsync (vtable 7)
	if err := page.call(7, mem.ptr(), opts.ptr(), uintptr(unsafe.Pointer(&action))); err != nil {
		return nil, fmt.Errorf("RenderWithOptionsToStreamAsync: %w", err)
	}
	defer action.release()
	if err := await(action, 120*time.Second); err != nil {
		return nil, fmt.Errorf("render page %d: %w", index+1, err)
	}

	// Read the PNG bytes back through a classic IStream.
	var istream comObj
	r, _, _ := procCreateStreamOverRandomAccessStream.Call(mem.ptr(), uintptr(unsafe.Pointer(&iidIStream)), uintptr(unsafe.Pointer(&istream)))
	if err := hresult(r, "CreateStreamOverRandomAccessStream"); err != nil {
		return nil, err
	}
	defer istream.release()
	var newPos uint64
	if err := istream.call(5, 0, 0, uintptr(unsafe.Pointer(&newPos))); err != nil { // Seek(0, STREAM_SEEK_SET)
		return nil, fmt.Errorf("seek: %w", err)
	}
	var buf bytes.Buffer
	chunk := make([]byte, 64<<10)
	for {
		var read uint32
		err := istream.call(3, uintptr(unsafe.Pointer(&chunk[0])), uintptr(len(chunk)), uintptr(unsafe.Pointer(&read))) // Read
		if read > 0 {
			buf.Write(chunk[:read])
		}
		if err != nil || read == 0 {
			break
		}
	}
	img, err := png.Decode(&buf)
	if err != nil {
		return nil, fmt.Errorf("decode rendered page: %w", err)
	}
	return img, nil
}
