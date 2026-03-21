package parser

import (
	. "github.com/nein-ar/dejot/ast"
)

func Parse(source []byte) *Document {
	doc := &Document{Source: source}
	bp := NewBlockParser(source)

	blockEvents := bp.Parse()

	ip := NewInlineParser(source)
	inlineEvents := ip.Parse(blockEvents)

	as := Assembler{doc: doc}
	as.Assemble(inlineEvents)
	doc.Events = inlineEvents

	return doc
}
