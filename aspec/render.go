package aspec

import (
	"bytes"
	"github.com/nein-ar/dejot/parser"
	"github.com/nein-ar/dejot/render/html"
	"github.com/nein-ar/dejot/render/txt"
)

func RenderTxt(srcPath string) (string, error) {
	expanded, p, err := Expand(srcPath)
	if err != nil {
		return "", err
	}

	doc := parser.Parse(expanded)
	renderer := txt.NewTxtRenderer(doc, p)

	var buf bytes.Buffer
	if err := renderer.Render(&buf); err != nil {
		return "", err
	}

	return buf.String(), nil
}

func RenderHTML(srcPath string) (string, error) {
	expanded, p, err := Expand(srcPath)
	if err != nil {
		return "", err
	}

	doc := parser.Parse(expanded)
	renderer := html.NewHTMLRenderer(doc, p)

	var buf bytes.Buffer
	if err := renderer.Render(&buf); err != nil {
		return "", err
	}

	return buf.String(), nil
}

func RenderDjot(srcPath string) (string, error) {
	expanded, _, err := Expand(srcPath)
	if err != nil {
		return "", err
	}

	return string(expanded), nil
}

func Validate(srcPath string) ([]ValidationError, error) {
	_, _, err := Expand(srcPath)
	if err != nil {
		return nil, err
	}

	return nil, nil
}
