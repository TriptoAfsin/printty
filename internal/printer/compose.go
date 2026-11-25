package printer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/filter"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// ComposeLayout builds the job as finished sheets: a new PDF with one page
// per sheet of paper, each source page placed, turned, scaled and clipped
// exactly as layout.go decides (the same rules the Windows backend draws
// with and the editor previews). Source pages stay vector: each becomes a
// form XObject, nothing is rasterised. The CUPS backend prints the result
// as it is, so binding margins, page order, turns and moved photos print on
// Linux and macOS too. It loads the whole PDF, so it runs in the worker.
//
// pages are 1-based, in printing order, 0 for an empty slot; nil means
// o.Pages. edge is the printer's unprintable edge in inches. It returns the
// number of sheets written to out.
func ComposeLayout(c context.Context, path, kind string, pages []int, o Options, edge float64, out string) (int, error) {
	if kind != "pdf" {
		// A single photo: wrap it in a PDF first, the way photo sets are.
		dir, err := os.MkdirTemp(filepath.Dir(out), "photo-")
		if err != nil {
			return 0, err
		}
		defer os.RemoveAll(dir)
		pdf := filepath.Join(dir, "photo.pdf")
		if _, err := ComposePhotos(c, []string{path}, pdf, dir, 0); err != nil {
			return 0, fmt.Errorf("wrap photo: %w", err)
		}
		path = pdf
	}

	model.ConfigPath = "disable" // never write a pdfcpu config folder
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	ctx, err := api.ReadContext(c, f, model.NewDefaultConfiguration())
	f.Close()
	if err != nil {
		return 0, fmt.Errorf("read PDF: %w", err)
	}
	if err := api.ValidateContext(c, ctx); err != nil {
		return 0, fmt.Errorf("read PDF: %w", err)
	}
	if err := ctx.EnsurePageCount(); err != nil {
		return 0, fmt.Errorf("read PDF: %w", err)
	}
	if len(pages) == 0 {
		if pages, err = ParsePages(o.Pages, ctx.PageCount); err != nil {
			return 0, err
		}
	}

	src := map[int]*formPage{}
	page := func(p int) *formPage {
		if fp, ok := src[p]; ok {
			return fp
		}
		fp := &formPage{}
		if p >= 1 && p <= ctx.PageCount {
			fp.load(c, ctx, p)
		}
		src[p] = fp
		return fp
	}
	size := func(p int) (float64, float64) {
		fp := page(p)
		return fp.w / 72, fp.h / 72
	}

	pw, ph := PaperSize(o.Paper)
	if PickOrientation(o, pages, size) == Landscape {
		pw, ph = ph, pw
	}
	o.BindEdge = edge
	sheets := Layout(pages, size, pw-2*edge, ph-2*edge, o)

	pagesDict := types.Dict{"Type": types.Name("Pages"), "Count": types.Integer(0), "Kids": types.Array{}}
	pagesRef, err := ctx.IndRefForNewObject(pagesDict)
	if err != nil {
		return 0, err
	}
	PW, PH := pw*72, ph*72
	for _, sh := range sheets {
		if err := c.Err(); err != nil {
			return 0, err
		}
		var buf bytes.Buffer
		forms := types.NewDict()
		for i, pl := range sh {
			fp := page(pl.Page)
			if err := fp.form(ctx); err != nil {
				return 0, fmt.Errorf("page %d: %w", pl.Page, err)
			}
			if fp.ref == nil {
				continue // a page with nothing on it
			}
			name := fmt.Sprintf("P%d", i)
			forms.Insert(name, *fp.ref)
			// Inches from the top-left of the printable area -> PDF points
			// from the bottom-left of the paper.
			cx, cy := (edge+pl.Cell.X)*72, PH-(edge+pl.Cell.Y+pl.Cell.H)*72
			x, y := (edge+pl.Rect.X)*72, PH-(edge+pl.Rect.Y+pl.Rect.H)*72
			w, h := pl.Rect.W*72, pl.Rect.H*72
			fmt.Fprintf(&buf, "q %.3f %.3f %.3f %.3f re W n %s /%s Do Q\n",
				cx, cy, pl.Cell.W*72, pl.Cell.H*72, placeMatrix(pl.Rotate, fp.w, fp.h, x, y, w, h), name)
		}
		if err := addSheet(ctx, pagesDict, pagesRef, forms, buf.Bytes(), PW, PH); err != nil {
			return 0, err
		}
	}
	root, err := ctx.Catalog()
	if err != nil {
		return 0, err
	}
	root.Update("Pages", *pagesRef)
	ctx.PageCount = len(sheets)
	if err := api.WriteContextFile(c, ctx, out); err != nil {
		return 0, fmt.Errorf("write PDF: %w", err)
	}
	return len(sheets), nil
}

// placeMatrix is the "cm" operator that puts a sw×sh form, turned rot
// degrees clockwise, into the x, y, w, h box (points, w×h already turned).
func placeMatrix(rot int, sw, sh, x, y, w, h float64) string {
	switch NormRotation(rot) {
	case 90:
		s := w / sh
		return fmt.Sprintf("0 %.6f %.6f 0 %.3f %.3f cm", -s, s, x, y+h)
	case 180:
		s := w / sw
		return fmt.Sprintf("%.6f 0 0 %.6f %.3f %.3f cm", -s, -s, x+w, y+h)
	case 270:
		s := w / sh
		return fmt.Sprintf("0 %.6f %.6f 0 %.3f %.3f cm", s, -s, x+w, y)
	}
	s := w / sw
	return fmt.Sprintf("%.6f 0 0 %.6f %.3f %.3f cm", s, s, x, y)
}

// formPage is one source page: its size as shown (points, after its own
// /Rotate) and, once needed, the form XObject that draws it.
type formPage struct {
	w, h  float64
	ref   *types.IndirectRef
	done  bool
	err   error
	pageC func(ctx *model.Context) (*types.IndirectRef, error)
}

func (fp *formPage) load(c context.Context, ctx *model.Context, p int) {
	fp.w, fp.h = 595.28, 841.89 // A4 until read
	d, _, attrs, err := ctx.PageDict(c, p, true)
	if err != nil || d == nil || attrs == nil || attrs.MediaBox == nil {
		fp.err = fmt.Errorf("can't read page %d", p)
		return
	}
	box := attrs.MediaBox.Clone()
	if attrs.CropBox != nil {
		box = attrs.CropBox.Clone()
	}
	turned := attrs.Rotate%180 != 0
	if turned {
		w := box.Width()
		box.UR.X = box.LL.X + box.Height()
		box.UR.Y = box.LL.Y + w
	}
	fp.w, fp.h = box.Width(), box.Height()
	fp.pageC = func(ctx *model.Context) (*types.IndirectRef, error) {
		content, err := ctx.PageContent(d, p)
		if errors.Is(err, model.ErrNoContent) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		if attrs.Rotate != 0 {
			content = append(model.ContentBytesForPageRotation(attrs.Rotate, box.Width(), box.Height()), content...)
		}
		res, err := ctx.IndRefForNewObject(attrs.Resources)
		if err != nil {
			return nil, err
		}
		sd := types.StreamDict{
			Dict: types.Dict{
				"Type":      types.Name("XObject"),
				"Subtype":   types.Name("Form"),
				"BBox":      box.Array(),
				"Matrix":    types.NewNumberArray(1, 0, 0, 1, -box.LL.X, -box.LL.Y),
				"Resources": *res,
			},
			Content:        content,
			FilterPipeline: []types.PDFFilter{{Name: filter.Flate}},
		}
		sd.InsertName("Filter", filter.Flate)
		if err := sd.Encode(); err != nil {
			return nil, err
		}
		return ctx.IndRefForNewObject(sd)
	}
}

// form makes the page's form XObject the first time it's placed; copies on
// one sheet and repeated pages share it.
func (fp *formPage) form(ctx *model.Context) error {
	if fp.done || fp.err != nil {
		return fp.err
	}
	fp.done = true
	if fp.pageC == nil {
		return nil
	}
	fp.ref, fp.err = fp.pageC(ctx)
	return fp.err
}

// addSheet appends one page (a sheet of paper) to the new page tree.
func addSheet(ctx *model.Context, pagesDict types.Dict, pagesRef *types.IndirectRef, forms types.Dict, content []byte, w, h float64) error {
	res, err := ctx.IndRefForNewObject(types.Dict{"XObject": forms})
	if err != nil {
		return err
	}
	sd, err := ctx.NewStreamDictForBuf(content)
	if err != nil {
		return err
	}
	if err := sd.Encode(); err != nil {
		return err
	}
	contents, err := ctx.IndRefForNewObject(*sd)
	if err != nil {
		return err
	}
	pg, err := ctx.IndRefForNewObject(types.Dict{
		"Type":      types.Name("Page"),
		"Parent":    *pagesRef,
		"MediaBox":  types.RectForDim(w, h).Array(),
		"Resources": *res,
		"Contents":  *contents,
	})
	if err != nil {
		return err
	}
	pagesDict.Update("Kids", append(pagesDict.ArrayEntry("Kids"), *pg))
	return pagesDict.IncrementBy("Count", 1)
}

// PaperSize is a paper's width and height in inches, portrait (A4 when the
// name is unknown). The editor's preview uses the same table (editor.js).
func PaperSize(name string) (w, h float64) {
	switch name {
	case "Letter":
		return 8.5, 11
	case "Legal":
		return 8.5, 14
	case "A5":
		return 5.83, 8.27
	case "4x6":
		return 4, 6
	case "5x7":
		return 5, 7
	}
	return 8.27, 11.69
}
