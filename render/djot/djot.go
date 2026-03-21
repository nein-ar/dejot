package djot

import (
	. "github.com/nein-ar/dejot/ast"
	"io"
)

type DjotRenderer struct {
	doc *Document
}

func NewDjotRenderer(doc *Document) *DjotRenderer {
	return &DjotRenderer{doc: doc}
}

func (r *DjotRenderer) Render(w io.Writer) error {
	_, err := w.Write(r.doc.Source)
	return err
}
