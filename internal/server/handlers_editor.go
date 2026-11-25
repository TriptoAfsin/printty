package server

import (
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/TriptoAfsin/printty/internal/printer"
	"github.com/TriptoAfsin/printty/internal/store"
)

// maxPreviews is how many pages get a preview image; longer documents show
// numbered tiles after that.
const maxPreviews = 60

// postUpload is the editor's first step: the file is saved as a draft with
// page previews, and the print page reopens on it to set up the layout.
func (s *Server) postUpload(w http.ResponseWriter, r *http.Request) {
	v, ok := s.needViewer(w, r)
	if !ok {
		return
	}
	if !s.limiter.allow("upload:"+clientIP(r), 60, time.Hour) {
		s.printError(w, r, v, nil, "Too many uploads. Try again in a while.")
		return
	}
	files, _, err := s.readForm(w, r, false)
	if err != nil {
		s.printError(w, r, v, nil, err.Error())
		return
	}
	j, err := s.makeJob(r.Context(), files)
	if err != nil {
		s.printError(w, r, v, nil, err.Error())
		return
	}
	s.saveDraft(j, v)
	http.Redirect(w, r, "/?draft="+j.ID, http.StatusSeeOther)
}

// getPagePreview serves one page preview of a job, to its owner or the admin.
func (s *Server) getPagePreview(w http.ResponseWriter, r *http.Request) {
	v := s.who(r)
	j, err := s.st.Job(r.PathValue("id"))
	if err != nil || !(s.owns(v, j) || v.IsAdmin()) {
		http.NotFound(w, r)
		return
	}
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil || n < 1 || n > maxPreviews {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(s.st.PreviewDir(j.ID), strconv.Itoa(n)+".png")
	f, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	io.Copy(w, f)
}

// previewPages lists the pages of j that have a preview image.
func (s *Server) previewPages(j *store.Job) []int {
	if j == nil {
		return nil
	}
	var out []int
	for p := 1; p <= j.DocPages && p <= maxPreviews; p++ {
		if _, err := os.Stat(filepath.Join(s.st.PreviewDir(j.ID), strconv.Itoa(p)+".png")); err == nil {
			out = append(out, p)
		}
	}
	return out
}

// parseLayout reads the layout and driver options from the form into o.
func parseLayout(f map[string]string, docPages int, caps printer.Caps, o *printer.Options) error {
	switch printer.Orientation(f["orientation"]) {
	case printer.Portrait:
		o.Orientation = printer.Portrait
	case printer.Landscape:
		o.Orientation = printer.Landscape
	}
	switch printer.Fit(f["fit"]) {
	case printer.Fill:
		o.Fit = printer.Fill
	case printer.Actual:
		o.Fit = printer.Actual
	}
	if f["persheet"] == "all" {
		pages, err := printer.ParsePages(o.Pages, docPages)
		if err != nil || len(pages) > printer.MaxPhotos {
			return fmt.Errorf("Up to %d pages fit on one sheet. Leave some out, or pick fewer per sheet.", printer.MaxPhotos)
		}
		o.OneSheet = true
		if spec := strings.TrimSpace(f["place"]); spec != "" {
			o.Place = map[int][2]float64{}
			for _, item := range strings.Split(spec, ",") {
				parts := strings.Split(strings.TrimSpace(item), ":")
				if len(parts) != 3 {
					return errors.New("Couldn't read where the photos go. Try again.")
				}
				p, err1 := strconv.Atoi(parts[0])
				x, err2 := strconv.ParseFloat(parts[1], 64)
				y, err3 := strconv.ParseFloat(parts[2], 64)
				if err1 != nil || err2 != nil || err3 != nil || p < 1 || p > docPages || math.IsNaN(x) || math.IsNaN(y) || len(o.Place) >= printer.MaxPhotos {
					return errors.New("Couldn't read where the photos go. Try again.")
				}
				o.Place[p] = [2]float64{round3(math.Max(0, math.Min(1, x))), round3(math.Max(0, math.Min(1, y)))}
			}
			if len(o.Place) == 0 {
				o.Place = nil
			}
		}
	} else if n, err := strconv.Atoi(f["persheet"]); err == nil {
		o.PerSheet = printer.PerSheet(printer.Options{PerSheet: n})
		if o.PerSheet == 1 {
			o.PerSheet = 0
		}
	}
	if n, err := strconv.Atoi(f["repeat"]); err == nil && !o.OneSheet {
		o.Repeat = printer.RepeatCount(printer.Options{Repeat: n})
		if o.Repeat == 1 {
			o.Repeat = 0
		}
	}
	if n, err := strconv.Atoi(f["scale"]); err == nil && n >= 10 && n < 100 {
		o.Scale = n
	}
	for key, dst := range map[string]*float64{"offx": &o.OffX, "offy": &o.OffY} {
		if v, err := strconv.ParseFloat(f[key], 64); err == nil && !math.IsNaN(v) {
			*dst = math.Max(-1, math.Min(1, round3(v)))
		}
	}
	// The checkbox is in every form; unticked sends nothing.
	if _, present := f["autorotate-shown"]; present {
		o.NoAutoRotate = f["autorotate"] != "on"
	}
	if spec := strings.TrimSpace(f["rotate"]); spec != "" {
		rot := map[int]int{}
		for _, pair := range strings.Split(spec, ",") {
			ps, ds, ok := strings.Cut(strings.TrimSpace(pair), ":")
			p, err1 := strconv.Atoi(ps)
			d, err2 := strconv.Atoi(ds)
			if !ok || err1 != nil || err2 != nil || p < 1 || p > docPages || len(rot) >= 1000 {
				return errors.New("Couldn't read the page turns. Try again.")
			}
			if d = printer.NormRotation(d); d != 0 {
				rot[p] = d
			}
		}
		if len(rot) > 0 {
			o.Rotate = rot
		}
	}
	for _, opt := range caps.Options {
		v, ok := f["drv_"+opt.Key]
		if !ok || !opt.Valid(v) || v == opt.Default {
			continue
		}
		if o.Driver == nil {
			o.Driver = map[string]string{}
		}
		o.Driver[opt.Key] = v
	}
	return nil
}

func round3(v float64) float64 { return math.Round(v*1000) / 1000 }
