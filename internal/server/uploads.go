package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/TriptoAfsin/printty/internal/memtrim"
	"github.com/TriptoAfsin/printty/internal/printer"
	"github.com/TriptoAfsin/printty/internal/store"
)

// One form post carries one file (a PDF or a photo) or several photos, which
// become one PDF with a page per photo.

// maxBody is the most one form post may carry: each file is at most maxUp,
// and several photos together at most three times that.
func (s *Server) maxBody() int64 { return 3*s.maxUp + 1<<20 }

// rawFile is an uploaded file saved under a random name, not yet a job.
type rawFile struct {
	path, kind, name string
	size             int64
}

func removeRaw(files []rawFile) {
	for _, f := range files {
		os.Remove(f.path)
	}
}

// readForm reads a multipart post: every file part (named "file", or any
// name with anyName) is saved, every other part becomes a field.
func (s *Server) readForm(w http.ResponseWriter, r *http.Request, anyName bool) ([]rawFile, map[string]string, error) {
	r.Body = http.MaxBytesReader(w, r.Body, s.maxBody())
	mr, err := r.MultipartReader()
	if err != nil {
		return nil, nil, errors.New("That upload didn't work. Try again.")
	}
	fields := map[string]string{}
	var files []rawFile
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			removeRaw(files)
			return nil, nil, s.tooBig(err)
		}
		if part.FileName() != "" {
			if !anyName && part.FormName() != "file" {
				continue
			}
			if len(files) >= printer.MaxPhotos {
				removeRaw(files)
				return nil, nil, fmt.Errorf("Choose up to %d photos at once.", printer.MaxPhotos)
			}
			f, err := s.saveRaw(part)
			if err != nil {
				removeRaw(files)
				return nil, nil, err
			}
			files = append(files, f)
			continue
		}
		if anyName || len(fields) >= 100 {
			continue
		}
		b, _ := io.ReadAll(io.LimitReader(part, 8192))
		fields[part.FormName()] = string(b)
	}
	return files, fields, nil
}

func (s *Server) tooBig(err error) error {
	var mb *http.MaxBytesError
	if errors.As(err, &mb) {
		return fmt.Errorf("That's over %d MB in one go. Send fewer photos at a time.", s.maxBody()>>20)
	}
	return errors.New("The upload was interrupted or too large.")
}

// saveRaw stores one file part under a random name after checking its type
// by its first bytes.
func (s *Server) saveRaw(part *multipart.Part) (rawFile, error) {
	head := make([]byte, 512)
	n, _ := io.ReadFull(part, head)
	head = head[:n]
	kind := sniff(head)
	if kind == "" {
		return rawFile{}, errBadFile
	}
	path := filepath.Join(s.st.FilesDir(), store.NewID()+"."+fileExt(kind))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return rawFile{}, err
	}
	size, err := io.Copy(f, io.MultiReader(strings.NewReader(string(head)), io.LimitReader(part, s.maxUp)))
	f.Close()
	if err != nil {
		os.Remove(path)
		return rawFile{}, s.tooBig(err)
	}
	if size > s.maxUp {
		os.Remove(path)
		return rawFile{}, fmt.Errorf("That file is over %d MB", s.maxUp>>20)
	}
	name := filepath.Base(strings.ReplaceAll(part.FileName(), "\\", "/"))
	if name == "" || name == "." || name == "/" {
		name = "document." + fileExt(kind)
	}
	return rawFile{path: path, kind: kind, name: name, size: size}, nil
}

func fileExt(kind string) string {
	return map[string]string{"pdf": "pdf", "png": "png", "jpeg": "jpg"}[kind]
}

var errNoFile = errors.New("Choose a PDF or photo to print.")

// makeJob turns the saved files into a draft job: one file as it is, several
// photos combined into one PDF. The raw files are gone either way on error.
func (s *Server) makeJob(ctx context.Context, files []rawFile) (store.Job, error) {
	switch len(files) {
	case 0:
		return store.Job{}, errNoFile
	case 1:
		return s.inspectFile(ctx, files[0])
	}
	for _, f := range files {
		if f.kind == "pdf" {
			removeRaw(files)
			return store.Job{}, errors.New("Choose one PDF, or several photos.")
		}
	}
	id := store.NewID()
	stored := id + ".pdf"
	out := filepath.Join(s.st.FilesDir(), stored)
	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = f.path
	}
	var pages int
	var err error
	if c, ok := s.be.(printer.Composer); ok {
		pages, err = c.Compose(ctx, paths, out, s.st.PreviewDir(id), maxPreviews)
	} else {
		pages, err = printer.ComposePhotos(ctx, paths, out, s.st.PreviewDir(id), maxPreviews)
	}
	removeRaw(files)
	memtrim.Now()
	if err != nil || pages != len(files) {
		os.Remove(out)
		os.RemoveAll(s.st.PreviewDir(id))
		return store.Job{}, errors.New("Those photos couldn't be combined. Is one of them damaged?")
	}
	var size int64
	if fi, err := os.Stat(out); err == nil {
		size = fi.Size()
	}
	name := files[0].name + " and " + strconv.Itoa(len(files)-1) + " more"
	return store.Job{ID: id, FileName: name, Kind: "pdf", StoredAs: stored, Size: size, DocPages: pages, Photos: pages,
		State: store.JobDraft, CreatedAt: time.Now(), UpdatedAt: time.Now()}, nil
}

// inspectFile counts a single file's pages and draws its previews.
func (s *Server) inspectFile(ctx context.Context, f rawFile) (store.Job, error) {
	id := store.NewID()
	stored := id + "." + fileExt(f.kind)
	path := filepath.Join(s.st.FilesDir(), stored)
	if err := os.Rename(f.path, path); err != nil {
		os.Remove(f.path)
		return store.Job{}, err
	}
	var pages int
	var err error
	if in, ok := s.be.(printer.Inspector); ok {
		// Count pages and draw the editor's previews in one worker run.
		pages, err = in.Inspect(ctx, path, f.kind, s.st.PreviewDir(id), maxPreviews)
	} else {
		pages, err = s.be.PageCount(ctx, path, f.kind)
	}
	memtrim.Now()
	if err != nil || pages < 1 {
		os.Remove(path)
		os.RemoveAll(s.st.PreviewDir(id))
		return store.Job{}, errors.New("That PDF couldn't be opened. Is it password-protected or damaged?")
	}
	return store.Job{ID: id, FileName: f.name, Kind: f.kind, StoredAs: stored, Size: f.size, DocPages: pages,
		State: store.JobDraft, CreatedAt: time.Now(), UpdatedAt: time.Now()}, nil
}
