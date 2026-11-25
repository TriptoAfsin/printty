//go:build windows

package printer

import (
	"context"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modWinspool = windows.NewLazySystemDLL("winspool.drv")
	modGdi32    = windows.NewLazySystemDLL("gdi32.dll")

	procEnumPrintersW       = modWinspool.NewProc("EnumPrintersW")
	procGetDefaultPrinterW  = modWinspool.NewProc("GetDefaultPrinterW")
	procOpenPrinterW        = modWinspool.NewProc("OpenPrinterW")
	procClosePrinter        = modWinspool.NewProc("ClosePrinter")
	procGetPrinterW         = modWinspool.NewProc("GetPrinterW")
	procEnumJobsW           = modWinspool.NewProc("EnumJobsW")
	procDocumentPropertiesW = modWinspool.NewProc("DocumentPropertiesW")
	procDeviceCapabilitiesW = modWinspool.NewProc("DeviceCapabilitiesW")

	procCreateDCW         = modGdi32.NewProc("CreateDCW")
	procDeleteDC          = modGdi32.NewProc("DeleteDC")
	procStartDocW         = modGdi32.NewProc("StartDocW")
	procEndDoc            = modGdi32.NewProc("EndDoc")
	procAbortDoc          = modGdi32.NewProc("AbortDoc")
	procStartPage         = modGdi32.NewProc("StartPage")
	procEndPage           = modGdi32.NewProc("EndPage")
	procGetDeviceCaps     = modGdi32.NewProc("GetDeviceCaps")
	procStretchDIBits     = modGdi32.NewProc("StretchDIBits")
	procSetStretchBltMode = modGdi32.NewProc("SetStretchBltMode")
)

const (
	printerEnumLocal       = 0x2
	printerEnumConnections = 0x4

	dmOutBuffer = 2
	dmInBuffer  = 8

	dmFieldPaperSize = 0x2
	dmFieldCopies    = 0x100
	dmFieldColor     = 0x800
	dmFieldDuplex    = 0x1000

	dmColorMono  = 1
	dmColorColor = 2
	dmDupSimplex = 1
	dmDupVert    = 2 // long edge
	dmDupHoriz   = 3 // short edge

	dcPapers     = 2
	dcDuplex     = 7
	dcPaperNames = 16
	dcColorDev   = 32

	capHorzRes    = 8
	capVertRes    = 10
	capLogPixelsX = 88
	capLogPixelsY = 90

	printerStatusPaused       = 0x1
	printerStatusError        = 0x2
	printerStatusPaperJam     = 0x8
	printerStatusPaperOut     = 0x10
	printerStatusOffline      = 0x80
	printerStatusNotAvailable = 0x1000
	printerStatusNoToner      = 0x40000
	printerStatusUserIntv     = 0x100000
	printerStatusDoorOpen     = 0x400000
	printerAttrWorkOffline    = 0x400

	jobStatusError    = 0x2
	jobStatusOffline  = 0x20
	jobStatusPaperOut = 0x40
	jobStatusBlocked  = 0x200
	jobStatusUserIntv = 0x400

	dibRGBColors = 0
	srcCopy      = 0x00CC0020
	halftone     = 4

	renderDPI = 300 // cap: higher printer DPIs are reached by GDI scaling
	bandRows  = 256
)

// devmodeW mirrors DEVMODEW (220 bytes) for the printer fields we use.
type devmodeW struct {
	DeviceName    [32]uint16
	SpecVersion   uint16
	DriverVersion uint16
	Size          uint16
	DriverExtra   uint16
	Fields        uint32
	Orientation   int16
	PaperSize     int16
	PaperLength   int16
	PaperWidth    int16
	Scale         int16
	Copies        int16
	DefaultSource int16
	PrintQuality  int16
	Color         int16
	Duplex        int16
	YResolution   int16
	TTOption      int16
	Collate       int16
	FormName      [32]uint16
	LogPixels     uint16
	BitsPerPel    uint32
	PelsWidth     uint32
	PelsHeight    uint32
	DisplayFlags  uint32
	DisplayFreq   uint32
	ICMMethod     uint32
	ICMIntent     uint32
	MediaType     uint32
	DitherType    uint32
	Reserved1     uint32
	Reserved2     uint32
	PanningWidth  uint32
	PanningHeight uint32
}

type printerInfo2 struct {
	ServerName, PrinterName, ShareName, PortName, DriverName, Comment, Location           *uint16
	DevMode                                                                               *devmodeW
	SepFile, PrintProcessor, Datatype, Parameters                                         *uint16
	SecurityDescriptor                                                                    uintptr
	Attributes, Priority, DefaultPriority, StartTime, UntilTime, Status, Jobs, AveragePPM uint32
}

type jobInfo1 struct {
	JobID                                                              uint32
	PrinterName, MachineName, UserName, Document, Datatype, StatusText *uint16
	Status, Priority, Position, TotalPages, PagesPrinted               uint32
	Submitted                                                          windows.Systemtime
}

type docInfoW struct {
	Size     int32
	DocName  *uint16
	Output   *uint16
	Datatype *uint16
	Type     uint32
}

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

// Windows is the Windows backend: Windows.Data.Pdf + the Win32 print API.
type Windows struct {
	mu sync.Mutex // one print job at a time through GDI
	// OutputFile, when set, sends jobs to this file (for "Microsoft Print to PDF" tests).
	OutputFile string
	// spooled remembers when each job was handed to the spooler and its progress.
	spooled map[string]*spoolTrack
	// noCaps skips DeviceCapabilities, which loads the printer driver into the process.
	noCaps bool
}

type spoolTrack struct {
	since        time.Time
	lastProgress time.Time
	printed      uint32
}

// New returns the backend for this OS.
func New() Backend { return &Windows{spooled: map[string]*spoolTrack{}} }

// NewLight returns a backend for the long-running server: printer names,
// status and the spooler only. It never loads printer drivers or the PDF engine.
func NewLight() Backend { return &Windows{spooled: map[string]*spoolTrack{}, noCaps: true} }

func utf16(s string) *uint16 {
	p, _ := windows.UTF16PtrFromString(s)
	return p
}

func defaultPrinter() string {
	var n uint32
	procGetDefaultPrinterW.Call(0, uintptr(unsafe.Pointer(&n)))
	if n == 0 {
		return ""
	}
	buf := make([]uint16, n)
	r, _, _ := procGetDefaultPrinterW.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n)))
	if r == 0 {
		return ""
	}
	return windows.UTF16ToString(buf)
}

func (w *Windows) Printers(ctx context.Context) ([]Info, error) {
	var needed, count uint32
	flags := uintptr(printerEnumLocal | printerEnumConnections)
	procEnumPrintersW.Call(flags, 0, 4, 0, 0, uintptr(unsafe.Pointer(&needed)), uintptr(unsafe.Pointer(&count)))
	if needed == 0 {
		return nil, nil
	}
	buf := make([]byte, needed)
	r, _, err := procEnumPrintersW.Call(flags, 0, 4, uintptr(unsafe.Pointer(&buf[0])), uintptr(needed), uintptr(unsafe.Pointer(&needed)), uintptr(unsafe.Pointer(&count)))
	if r == 0 {
		return nil, fmt.Errorf("EnumPrinters: %w", err)
	}
	// PRINTER_INFO_4: pPrinterName, pServerName, Attributes
	type info4 struct {
		PrinterName, ServerName *uint16
		Attributes              uint32
	}
	def := defaultPrinter()
	size := unsafe.Sizeof(info4{})
	var out []Info
	for i := uint32(0); i < count; i++ {
		p := (*info4)(unsafe.Pointer(&buf[uintptr(i)*size]))
		name := windows.UTF16PtrToString(p.PrinterName)
		info := Info{Name: name, Default: name == def}
		if !w.noCaps {
			info.Caps = capsFor(name)
		}
		out = append(out, info)
	}
	return out, nil
}

func deviceCaps(name string, cap uint16, out unsafe.Pointer) int32 {
	r, _, _ := procDeviceCapabilitiesW.Call(uintptr(unsafe.Pointer(utf16(name))), 0, uintptr(cap), uintptr(out), 0)
	return int32(r)
}

// paperIDs maps the driver's paper names to its DMPAPER ids.
func paperIDs(name string) map[string]int16 {
	n := deviceCaps(name, dcPapers, nil)
	if n <= 0 || n > 512 {
		return nil
	}
	ids := make([]uint16, n)
	names := make([]uint16, int(n)*64)
	deviceCaps(name, dcPapers, unsafe.Pointer(&ids[0]))
	deviceCaps(name, dcPaperNames, unsafe.Pointer(&names[0]))
	m := map[string]int16{}
	for i := 0; i < int(n); i++ {
		m[windows.UTF16ToString(names[i*64:(i+1)*64])] = int16(ids[i])
	}
	return m
}

// matchPaper finds the driver paper id for one of Printty's paper names.
func matchPaper(ids map[string]int16, want string) (int16, bool) {
	std := map[string]int16{"A4": 9, "Letter": 1, "Legal": 5, "A5": 11}
	keys := map[string][]string{
		"A4": {"A4"}, "Letter": {"Letter"}, "Legal": {"Legal"}, "A5": {"A5"},
		"4x6": {"4\"x6\"", "4x6", "10x15", "KG"}, "5x7": {"5\"x7\"", "5x7", "13x18", "2L"},
	}
	for drvName, id := range ids {
		for _, k := range keys[want] {
			if strings.HasPrefix(drvName, k) && !strings.Contains(drvName, "Scaled") {
				return id, true
			}
		}
	}
	id, ok := std[want]
	return id, ok
}

func capsFor(name string) Caps {
	c := Caps{Color: deviceCaps(name, dcColorDev, nil) == 1, DefaultPaper: "A4"}
	// DC_DUPLEX reports 1 for Canon's manual-duplex drivers too, so treat
	// duplex as manual unless the driver is known to have a duplex unit.
	// Printty runs manual duplex by default; an admin setting can override.
	c.AutoDuplex = false
	ids := paperIDs(name)
	for _, p := range PaperNames {
		if _, ok := matchPaper(ids, p); ok {
			c.Papers = append(c.Papers, p)
		}
	}
	if len(c.Papers) == 0 {
		c.Papers = []string{"A4", "Letter"}
	}
	c.Options = driverOptions(name)
	return c
}

func openPrinter(name string) (windows.Handle, error) {
	var h windows.Handle
	r, _, err := procOpenPrinterW.Call(uintptr(unsafe.Pointer(utf16(name))), uintptr(unsafe.Pointer(&h)), 0)
	if r == 0 {
		return 0, fmt.Errorf("OpenPrinter %q: %w", name, err)
	}
	return h, nil
}

func closePrinter(h windows.Handle) { procClosePrinter.Call(uintptr(h)) }

func (w *Windows) Status(ctx context.Context, name string) (Status, error) {
	h, err := openPrinter(name)
	if err != nil {
		return Status{State: Unknown, Detail: "Printer not found"}, err
	}
	defer closePrinter(h)
	var needed uint32
	procGetPrinterW.Call(uintptr(h), 2, 0, 0, uintptr(unsafe.Pointer(&needed)))
	if needed == 0 {
		return Status{State: Unknown}, fmt.Errorf("GetPrinter: no data")
	}
	buf := make([]byte, needed)
	r, _, e := procGetPrinterW.Call(uintptr(h), 2, uintptr(unsafe.Pointer(&buf[0])), uintptr(needed), uintptr(unsafe.Pointer(&needed)))
	if r == 0 {
		return Status{State: Unknown}, fmt.Errorf("GetPrinter: %w", e)
	}
	pi := (*printerInfo2)(unsafe.Pointer(&buf[0]))
	st := Status{State: Ready, OSSays: "Idle", JobsInOS: int(pi.Jobs)}
	s := pi.Status
	switch {
	case pi.Attributes&printerAttrWorkOffline != 0 || s&(printerStatusOffline|printerStatusNotAvailable) != 0:
		st.State, st.OSSays, st.Detail = Offline, "Offline", "Windows reports the printer offline."
	case s&printerStatusPaperOut != 0:
		st.State, st.OSSays, st.Detail = Error, "Paper out", "Load paper."
	case s&printerStatusPaperJam != 0:
		st.State, st.OSSays, st.Detail = Error, "Paper jam", "Clear the paper jam."
	case s&printerStatusDoorOpen != 0:
		st.State, st.OSSays, st.Detail = Error, "Cover open", "Close the printer cover."
	case s&printerStatusNoToner != 0:
		st.State, st.OSSays, st.Detail = Error, "Out of ink", "Refill the ink."
	case s&(printerStatusError|printerStatusUserIntv) != 0:
		st.State, st.OSSays, st.Detail = Error, "Needs attention", "The printer needs attention."
	case s&printerStatusPaused != 0:
		st.State, st.OSSays, st.Detail = Error, "Paused", "Printing is paused in Windows."
	}
	// Jobs stuck in the spooler: the main sign a USB printer is switched off.
	stuck := 0
	for _, j := range enumJobs(h) {
		if j.Status&(jobStatusError|jobStatusOffline|jobStatusPaperOut|jobStatusBlocked|jobStatusUserIntv) != 0 {
			stuck++
		}
	}
	switch {
	case pi.Jobs == 0:
		st.Spooler = "Clear"
	case stuck > 0:
		st.Spooler = fmt.Sprintf("%d waiting, %d stuck", pi.Jobs, stuck)
		if st.State == Ready {
			st.State, st.Detail = Offline, "Jobs are stuck in the queue. The printer is probably off."
		}
	default:
		st.Spooler = fmt.Sprintf("%d printing", pi.Jobs)
	}
	return st, nil
}

type osJob struct {
	ID       uint32
	Document string
	Status   uint32
	Printed  uint32
	Total    uint32
}

func enumJobs(h windows.Handle) []osJob {
	var needed, count uint32
	procEnumJobsW.Call(uintptr(h), 0, 255, 1, 0, 0, uintptr(unsafe.Pointer(&needed)), uintptr(unsafe.Pointer(&count)))
	if needed == 0 {
		return nil
	}
	buf := make([]byte, needed)
	r, _, _ := procEnumJobsW.Call(uintptr(h), 0, 255, 1, uintptr(unsafe.Pointer(&buf[0])), uintptr(needed), uintptr(unsafe.Pointer(&needed)), uintptr(unsafe.Pointer(&count)))
	if r == 0 {
		return nil
	}
	size := unsafe.Sizeof(jobInfo1{})
	out := make([]osJob, 0, count)
	for i := uint32(0); i < count; i++ {
		j := (*jobInfo1)(unsafe.Pointer(&buf[uintptr(i)*size]))
		out = append(out, osJob{ID: j.JobID, Document: windows.UTF16PtrToString(j.Document), Status: j.Status, Printed: j.PagesPrinted, Total: j.TotalPages})
	}
	return out
}

func docName(jobID string) string { return "Printty " + jobID }

// stuckAfter is how long a spooled job may sit without progress before
// Printty decides the printer is off.
const stuckAfter = 45 * time.Second

func (w *Windows) Spool(ctx context.Context, printer, jobID string) (SpoolState, error) {
	h, err := openPrinter(printer)
	if err != nil {
		return SpoolWaiting, err
	}
	defer closePrinter(h)
	w.mu.Lock()
	tr := w.spooled[jobID]
	if tr == nil {
		tr = &spoolTrack{since: time.Now(), lastProgress: time.Now()}
		w.spooled[jobID] = tr
	}
	w.mu.Unlock()
	prefix := docName(jobID)
	for _, j := range enumJobs(h) {
		if !strings.HasPrefix(j.Document, prefix) {
			continue
		}
		if j.Printed > tr.printed {
			tr.printed, tr.lastProgress = j.Printed, time.Now()
		}
		if j.Status&(jobStatusError|jobStatusOffline|jobStatusPaperOut|jobStatusBlocked|jobStatusUserIntv) != 0 {
			return SpoolStuck, nil
		}
		if time.Since(tr.lastProgress) > stuckAfter {
			return SpoolStuck, nil
		}
		return SpoolWaiting, nil
	}
	w.mu.Lock()
	delete(w.spooled, jobID)
	w.mu.Unlock()
	return SpoolGone, nil
}

func (w *Windows) PageCount(ctx context.Context, path, kind string) (int, error) {
	if kind != "pdf" {
		return 1, nil
	}
	n := 0
	err := withWinRT(func() error {
		doc, err := openPDF(path)
		if err != nil {
			return err
		}
		defer doc.close()
		n = doc.pages
		return nil
	})
	return n, err
}

// devmodeFor returns a DEVMODE for the printer with the job's options merged in.
func devmodeFor(h windows.Handle, name string, o Options, orient Orientation) ([]byte, error) {
	pname := utf16(name)
	size, _, _ := procDocumentPropertiesW.Call(0, uintptr(h), uintptr(unsafe.Pointer(pname)), 0, 0, 0)
	if int32(size) <= 0 {
		return nil, fmt.Errorf("DocumentProperties size failed")
	}
	buf := make([]byte, size)
	r, _, _ := procDocumentPropertiesW.Call(0, uintptr(h), uintptr(unsafe.Pointer(pname)), uintptr(unsafe.Pointer(&buf[0])), 0, dmOutBuffer)
	if int32(r) < 0 {
		return nil, fmt.Errorf("DocumentProperties read failed")
	}
	dm := (*devmodeW)(unsafe.Pointer(&buf[0]))
	dm.Fields |= dmFieldCopies | dmFieldColor | dmFieldDuplex
	dm.Copies = 1 // Printty repeats pages itself, so drivers can't double up
	if o.Color {
		dm.Color = dmColorColor
	} else {
		dm.Color = dmColorMono
	}
	dm.Duplex = dmDupSimplex // manual duplex is done by Printty; auto duplex below
	switch o.Duplex {
	case LongEdge:
		if capsFor(name).AutoDuplex {
			dm.Duplex = dmDupVert
		}
	case ShortEdge:
		if capsFor(name).AutoDuplex {
			dm.Duplex = dmDupHoriz
		}
	}
	if id, ok := matchPaper(paperIDs(name), o.Paper); ok {
		dm.Fields |= dmFieldPaperSize
		dm.PaperSize = id
	}
	applyLayoutAndDriver(dm, orient, o)
	r, _, _ = procDocumentPropertiesW.Call(0, uintptr(h), uintptr(unsafe.Pointer(pname)), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&buf[0])), dmInBuffer|dmOutBuffer)
	if int32(r) < 0 {
		return nil, fmt.Errorf("DocumentProperties merge failed")
	}
	return buf, nil
}

func getCaps(hdc uintptr, idx int) int {
	r, _, _ := procGetDeviceCaps.Call(hdc, uintptr(idx))
	return int(int32(r))
}

// pageSource yields page images one at a time so only one page is in memory.
type pageSource struct {
	count int
	size  func(i int) (wIn, hIn float64) // page size in inches
	draw  func(i, wPx, hPx int) (image.Image, error)
	close func()
}

func (w *Windows) Print(ctx context.Context, job Job) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return withWinRT(func() error { return w.printLocked(ctx, job) })
}

func (w *Windows) printLocked(ctx context.Context, job Job) error {
	src, err := openSource(job)
	if err != nil {
		return err
	}
	defer src.close()

	pages := job.PageList
	if len(pages) == 0 {
		pages, err = ParsePages(job.Options.Pages, src.count)
		if err != nil {
			return err
		}
	}

	// The sheet orientation follows the first page, or all of them on one
	// sheet (see layout.go).
	orient := PickOrientation(job.Options, pages, func(p int) (float64, float64) { return src.size(p - 1) })

	h, err := openPrinter(job.Printer)
	if err != nil {
		return err
	}
	dm, err := devmodeFor(h, job.Printer, job.Options, orient)
	closePrinter(h)
	if err != nil {
		return err
	}
	hdc, _, e := procCreateDCW.Call(uintptr(unsafe.Pointer(utf16("WINSPOOL"))), uintptr(unsafe.Pointer(utf16(job.Printer))), 0, uintptr(unsafe.Pointer(&dm[0])))
	if hdc == 0 {
		return fmt.Errorf("CreateDC: %w", e)
	}
	defer procDeleteDC.Call(hdc)
	procSetStretchBltMode.Call(hdc, halftone)

	horz, vert := getCaps(hdc, capHorzRes), getCaps(hdc, capVertRes)
	dpiX, dpiY := getCaps(hdc, capLogPixelsX), getCaps(hdc, capLogPixelsY)
	if horz <= 0 || vert <= 0 || dpiX <= 0 || dpiY <= 0 {
		return fmt.Errorf("printer reported no printable area")
	}

	di := docInfoW{DocName: utf16(docName(job.ID) + " " + job.Title)}
	di.Size = int32(unsafe.Sizeof(di))
	if w.OutputFile != "" {
		di.Output = utf16(w.OutputFile)
	} else if dir := os.Getenv("PRINTTY_TEST_OUTPUT_DIR"); dir != "" {
		// Test mode: print into files (use with "Microsoft Print to PDF").
		di.Output = utf16(filepath.Join(dir, job.ID+"-"+time.Now().Format("150405.000")+".pdf"))
	}
	if r, _, e := procStartDocW.Call(hdc, uintptr(unsafe.Pointer(&di))); int32(r) <= 0 {
		return fmt.Errorf("StartDoc: %w", e)
	}
	ok := false
	defer func() {
		if !ok {
			procAbortDoc.Call(hdc)
		}
	}()

	areaW, areaH := float64(horz)/float64(dpiX), float64(vert)/float64(dpiY)
	sheets := Layout(pages, func(p int) (float64, float64) { return src.size(p - 1) }, areaW, areaH, job.Options)
	maxDPI := float64(renderDPI)
	if job.Options.Driver["quality"] == "draft" {
		maxDPI = 150
	}
	copies := job.Options.Copies
	if copies < 1 {
		copies = 1
	}
	for c := 0; c < copies; c++ {
		for _, sh := range sheets {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := drawSheet(hdc, src, sh, dpiX, dpiY, maxDPI); err != nil {
				return err
			}
		}
	}
	if r, _, e := procEndDoc.Call(hdc); int32(r) <= 0 {
		return fmt.Errorf("EndDoc: %w", e)
	}
	ok = true
	w.spooled[job.ID] = &spoolTrack{since: time.Now(), lastProgress: time.Now()}
	return nil
}

// fillBGRA writes rows [y0, y0+rows) of img as BGRA over white.
func fillBGRA(dst []byte, img image.Image, y0, rows int) {
	b := img.Bounds()
	i := 0
	for y := y0; y < y0+rows; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, a := img.At(x, y).RGBA()
			if a < 0xffff { // composite on white paper
				inv := 0xffff - a
				r, g, bl = r+inv, g+inv, bl+inv
			}
			dst[i], dst[i+1], dst[i+2], dst[i+3] = byte(bl>>8), byte(g>>8), byte(r>>8), 0
			i += 4
		}
	}
}

func openSource(job Job) (*pageSource, error) {
	switch job.Kind {
	case "pdf":
		doc, err := openPDF(job.Path)
		if err != nil {
			return nil, err
		}
		return &pageSource{
			count: doc.pages,
			size: func(i int) (float64, float64) {
				w, h, err := doc.pageSize(i)
				if err != nil || w <= 0 || h <= 0 {
					return 8.27, 11.69
				}
				return float64(w) / 96, float64(h) / 96
			},
			draw:  doc.render,
			close: doc.close,
		}, nil
	case "png", "jpeg":
		f, err := os.Open(job.Path)
		if err != nil {
			return nil, err
		}
		img, _, err := image.Decode(f)
		f.Close()
		if err != nil {
			return nil, fmt.Errorf("decode image: %w", err)
		}
		b := img.Bounds()
		return &pageSource{
			count: 1,
			size:  func(int) (float64, float64) { return float64(b.Dx()) / 300, float64(b.Dy()) / 300 },
			draw:  func(int, int, int) (image.Image, error) { return img, nil },
			close: func() {},
		}, nil
	}
	return nil, ErrUnsupported
}

func selfTest(path string) (string, error) {
	var msg string
	err := withWinRT(func() error {
		doc, err := openPDF(path)
		if err != nil {
			return fmt.Errorf("open PDF: %w", err)
		}
		defer doc.close()
		if doc.pages != 2 {
			return fmt.Errorf("expected 2 pages, got %d", doc.pages)
		}
		img, err := doc.render(0, 248, 351)
		if err != nil {
			return fmt.Errorf("render: %w", err)
		}
		msg = fmt.Sprintf("Windows PDF engine works: opened 2 pages, rendered page 1 at %dx%d.", img.Bounds().Dx(), img.Bounds().Dy())
		return nil
	})
	return msg, err
}
