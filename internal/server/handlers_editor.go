package server

import (
	"errors"
	"io"
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
	r.Body = http.MaxBytesReader(w, r.Body, s.maxUp+(1<<20))
	mr, err := r.MultipartReader()
	if err != nil {
		s.printError(w, r, v, nil, "That upload didn't work. Try again.")
		return
	}
	for {
		part, err := mr.NextPart()
		if err != nil {
			break
		}
		if part.FormName() != "file" || part.FileName() == "" {
			continue
		}
		j, err := s.saveUpload(r.Context(), part, part.FileName())
		if err != nil {
			s.printError(w, r, v, nil, err.Error())
			return
		}
		s.saveDraft(j, v)
		http.Redirect(w, r, "/?draft="+j.ID, http.StatusSeeOther)
		return
	}
	s.printError(w, r, v, nil, "Choose a PDF or photo to print.")
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
	if n, err := strconv.Atoi(f["persheet"]); err == nil {
		o.PerSheet = printer.PerSheet(printer.Options{PerSheet: n})
		if o.PerSheet == 1 {
			o.PerSheet = 0
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
