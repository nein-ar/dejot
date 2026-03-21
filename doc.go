package dejot

import (
	"github.com/nein-ar/dejot/ast"
	"github.com/nein-ar/dejot/parser"
)

func Parse(source []byte) *ast.Document {
	return parser.Parse(source)
}
