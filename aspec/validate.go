package aspec

import (
	. "github.com/nein-ar/dejot/ast"
)

type ValidationError struct {
	Type    string
	Message string
	Target  string
}

func ValidateReferences(doc *Document) []ValidationError {
	return nil
}
