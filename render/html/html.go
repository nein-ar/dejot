package html

import (
	"bytes"
	"log"
	"strconv"
	"strings"

	. "github.com/nein-ar/dejot/ast"
	"github.com/nein-ar/dejot/params"
	"io"
)

type HTMLRenderer struct {
	doc        *Document
	tight      bool
	indentStep int
}

func init() {
	params.RegisterRenderer("html", []string{"indent"})
}

func NewHTMLRenderer(doc *Document, p params.Params) *HTMLRenderer {
	indentStep := 0
	if v := p.Get("indent", ""); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			indentStep = n
		}
	}
	for k := range p {
		if !params.SupportedBy("html", k) {
			log.Printf("aspec/html: unsupported param %q", k)
		}
	}
	return &HTMLRenderer{doc: doc, indentStep: indentStep}
}

func (r *HTMLRenderer) Render(w io.Writer) error {
	if len(r.doc.Nodes) == 0 {
		return nil
	}
	err := r.renderNode(w, 0)
	if err != nil {
		return err
	}
	return r.renderFootnotes(w)
}

func (r *HTMLRenderer) getBytes(start, end int32) []byte {
	if start == -1 && end == -1 {
		return nil
	}
	if start < 0 {
		idx := ^start
		if idx < 0 || idx > int32(len(r.doc.Extra))-1 || end < 0 || end < idx || end >= int32(len(r.doc.Extra)) {
			return nil
		}
		return r.doc.Extra[idx : end+1]
	}
	if start >= int32(len(r.doc.Source)) || end >= int32(len(r.doc.Source)) || start > end {
		return nil
	}
	return r.doc.Source[start : end+1]
}

func (r *HTMLRenderer) getImageAltText(parentIdx, excludeIdx int32) string {
	var result strings.Builder
	curr := r.doc.Nodes[parentIdx].Child
	for curr != -1 {
		if curr != excludeIdx {
			r.extractPlainText(&result, curr)
		}
		curr = r.doc.Nodes[curr].Next
	}
	return result.String()
}

func (r *HTMLRenderer) extractPlainText(w *strings.Builder, idx int32) {
	node := r.doc.Nodes[idx]
	switch node.Type {
	case NodeStr:
		bytes := r.getBytes(node.Start, node.End)
		w.Write(bytes)
	case NodeSoftBreak:
		w.WriteRune(' ')
	case NodeHardBreak:
		w.WriteRune('\n')
	case NodeNonBreakingSpace:
		w.WriteRune(' ')
	default:
		curr := node.Child
		for curr != -1 {
			r.extractPlainText(w, curr)
			curr = r.doc.Nodes[curr].Next
		}
	}
}

func (r *HTMLRenderer) renderNode(w io.Writer, idx int32) error {
	node := r.doc.Nodes[idx]
	switch node.Type {
	case NodeDoc:
		r.renderChildren(w, idx, -1)
	case NodeSection:
		w.Write([]byte("<section"))
		r.renderAttributes(w, idx)
		w.Write([]byte(">\n"))
		r.renderChildren(w, idx, -1)
		w.Write([]byte("</section>\n"))
	case NodePara:
		if r.tight {
			r.renderChildren(w, idx, -1)
			w.Write([]byte("\n"))
		} else {
			w.Write([]byte("<p"))
			r.renderAttributes(w, idx)
			w.Write([]byte(">"))
			r.renderChildren(w, idx, -1)
			w.Write([]byte("</p>\n"))
		}
	case NodeHeading:
		level := node.Level
		if level == 0 {
			level = int16(node.Data & 0xFFFF)
		}
		w.Write([]byte("<h"))
		w.Write([]byte(strconv.Itoa(int(level))))
		r.renderAttributes(w, idx)
		w.Write([]byte(">"))
		r.renderChildren(w, idx, -1)
		w.Write([]byte("</h"))
		w.Write([]byte(strconv.Itoa(int(level))))
		w.Write([]byte(">\n"))
	case NodeThematicBreak:
		w.Write([]byte("<hr"))
		r.renderAttributes(w, idx)
		w.Write([]byte(">\n"))
	case NodeBlockQuote:
		w.Write([]byte("<blockquote"))
		r.renderAttributes(w, idx)
		w.Write([]byte(">\n"))
		r.renderChildren(w, idx, -1)
		w.Write([]byte("</blockquote>\n"))
	case NodeBulletList, NodeTaskList:
		w.Write([]byte("<ul"))
		if node.Type == NodeTaskList {
			hasTaskListClass := false
			if node.Attr != -1 {
				for j := uint16(0); j < node.AttrCount; j++ {
					attr := r.doc.Attributes[node.Attr+int32(j)]
					if attr.KeyStart == -2 {
						val := string(r.getAttrVal(attr))
						if strings.Contains(val, "task-list") {
							hasTaskListClass = true
							break
						}
					}
				}
			}
			if !hasTaskListClass {
				w.Write([]byte(" class=\"task-list\""))
			}
		}
		r.renderAttributes(w, idx)
		w.Write([]byte(">\n"))
		oldTight := r.tight
		r.tight = node.Data&DataListTight != 0
		r.renderChildren(w, idx, -1)
		r.tight = oldTight
		w.Write([]byte("</ul>\n"))
	case NodeOrderedList:
		w.Write([]byte("<ol"))
		startNum := int(node.Data >> 16)
		if startNum != 0 && startNum != 1 {
			w.Write([]byte(" start=\""))
			w.Write([]byte(strconv.Itoa(startNum)))
			w.Write([]byte("\""))
		}
		listType := ""
		if node.Data&DataListLowerAlpha != 0 {
			listType = "a"
		} else if node.Data&DataListUpperAlpha != 0 {
			listType = "A"
		} else if node.Data&DataListLowerRoman != 0 {
			listType = "i"
		} else if node.Data&DataListUpperRoman != 0 {
			listType = "I"
		}
		if listType != "" {
			w.Write([]byte(" type=\""))
			w.Write([]byte(listType))
			w.Write([]byte("\""))
		}
		r.renderAttributes(w, idx)
		w.Write([]byte(">\n"))
		oldTight := r.tight
		r.tight = node.Data&DataListTight != 0
		r.renderChildren(w, idx, -1)
		r.tight = oldTight
		w.Write([]byte("</ol>\n"))
	case NodeDefinitionList:
		w.Write([]byte("<dl"))
		r.renderAttributes(w, idx)
		w.Write([]byte(">\n"))
		oldTight := r.tight
		r.tight = node.Data&DataListTight != 0
		r.renderChildren(w, idx, -1)
		r.tight = oldTight
		w.Write([]byte("</dl>\n"))
	case NodeDefinitionListItem:
		r.renderChildren(w, idx, -1)
	case NodeTerm:
		w.Write([]byte("<dt"))
		r.renderAttributes(w, idx)
		w.Write([]byte(">"))
		r.renderChildren(w, idx, -1)
		w.Write([]byte("</dt>\n"))
	case NodeDefinition:
		w.Write([]byte("<dd"))
		r.renderAttributes(w, idx)
		w.Write([]byte(">\n"))
		r.renderChildren(w, idx, -1)
		w.Write([]byte("</dd>\n"))
	case NodeListItem, NodeTaskListItem:
		w.Write([]byte("<li"))
		r.renderAttributes(w, idx)
		w.Write([]byte(">\n"))
		if node.Type == NodeTaskListItem {
			if node.Data&DataTaskChecked != 0 {
				w.Write([]byte("<input disabled=\"\" type=\"checkbox\" checked=\"\"/>"))
			} else {
				w.Write([]byte("<input disabled=\"\" type=\"checkbox\"/>"))
			}
			w.Write([]byte("\n"))
		}
		r.renderChildren(w, idx, -1)
		w.Write([]byte("</li>\n"))
	case NodeTable:
		w.Write([]byte("<table"))
		r.renderAttributes(w, idx)
		w.Write([]byte(">\n"))
		captionIdx := r.findChildType(idx, NodeCaption)
		if captionIdx != -1 {
			r.renderNode(w, captionIdx)
		}
		r.renderChildren(w, idx, captionIdx)
		w.Write([]byte("</table>\n"))
	case NodeRow:
		w.Write([]byte("<tr>\n"))
		r.renderChildren(w, idx, -1)
		w.Write([]byte("</tr>\n"))
	case NodeCell:
		tag := "td"
		if node.Data&DataCellHeader != 0 {
			tag = "th"
		}
		w.Write([]byte("<"))
		w.Write([]byte(tag))
		if node.Data&(DataAlignLeft|DataAlignCenter|DataAlignRight) != 0 {
			w.Write([]byte(" style=\"text-align: "))
			if node.Data&DataAlignCenter != 0 {
				w.Write([]byte("center"))
			} else if node.Data&DataAlignLeft != 0 {
				w.Write([]byte("left"))
			} else {
				w.Write([]byte("right"))
			}
			w.Write([]byte(";\""))
		}
		r.renderAttributes(w, idx)
		w.Write([]byte(">"))
		r.renderChildren(w, idx, -1)
		w.Write([]byte("</"))
		w.Write([]byte(tag))
		w.Write([]byte(">\n"))
	case NodeCaption:
		w.Write([]byte("<caption>"))
		r.renderChildren(w, idx, -1)
		w.Write([]byte("</caption>\n"))
	case NodeCodeBlock:
		w.Write([]byte("<pre"))
		if node.Attr != -1 {
			for j := uint16(0); j < node.AttrCount; j++ {
				attr := r.doc.Attributes[node.Attr+int32(j)]
				if attr.KeyStart != -2 {
					r.renderAttribute(w, attr)
				}
			}
		}
		w.Write([]byte("><code"))

		hasLanguage := false
		var language string
		if node.Attr != -1 {
			for j := uint16(0); j < node.AttrCount; j++ {
				attr := r.doc.Attributes[node.Attr+int32(j)]
				if attr.KeyStart == -2 {
					val := string(r.getAttrVal(attr))
					if !hasLanguage {
						hasLanguage = true
						language = val
						if strings.Contains(language, " ") {
							parts := strings.SplitN(language, " ", 2)
							language = parts[0]
						}
					}
				}
			}
		}
		if hasLanguage {
			w.Write([]byte(" class=\"language-"))
			w.Write([]byte(language))
			w.Write([]byte("\""))
		}
		w.Write([]byte(">"))
		content := r.getBytes(node.Start, node.End)
		if content != nil {
			w.Write(escapeHTML(content))
		}
		w.Write([]byte("</code></pre>\n"))
	case NodeVerbatim:
		w.Write([]byte("<code"))
		if node.Attr != -1 {
			for j := uint16(0); j < node.AttrCount; j++ {
				attr := r.doc.Attributes[node.Attr+int32(j)]
				if attr.KeyStart == -3 {
					val := r.getAttrVal(attr)
					if len(val) > 0 {
						w.Write([]byte(" class=\"language-"))
						w.Write(escapeHTML(val))
						w.Write([]byte("\""))
					}
					break
				}
			}
		}
		r.renderAttributes(w, idx)
		w.Write([]byte(">"))
		w.Write(escapeHTMLText(r.getBytes(node.Start, node.End)))
		w.Write([]byte("</code>"))
	case NodeRawInline, NodeRawBlock:
		format := ""
		if node.Attr != -1 {
			for j := uint16(0); j < node.AttrCount; j++ {
				attr := r.doc.Attributes[node.Attr+int32(j)]
				if attr.KeyStart == -3 {
					val := r.getAttrVal(attr)
					if len(val) > 0 && val[0] == '=' {
						format = string(val[1:])
					} else {
						format = string(val)
					}
					break
				}
			}
		}
		if format == "html" {
			w.Write(r.getBytes(node.Start, node.End))
		}
	case NodeDiv:
		w.Write([]byte("<div"))
		r.renderAttributes(w, idx)
		w.Write([]byte(">\n"))
		r.renderChildren(w, idx, -1)
		w.Write([]byte("</div>\n"))
	case NodeInlineMath:
		w.Write([]byte("<span class=\"math inline\">\\("))
		w.Write(escapeHTML(r.getBytes(node.Start, node.End)))
		w.Write([]byte("\\)</span>"))
	case NodeDisplayMath:
		w.Write([]byte("<span class=\"math display\">\\["))
		w.Write(escapeHTML(r.getBytes(node.Start, node.End)))
		w.Write([]byte("\\]</span>"))
	case NodeStr:
		if node.Attr != -1 {
			w.Write([]byte("<span"))
			r.renderAttributes(w, idx)
			w.Write([]byte(">"))
			w.Write(escapeHTMLText(r.getBytes(node.Start, node.End)))
			w.Write([]byte("</span>"))
		} else {
			w.Write(escapeHTMLText(r.getBytes(node.Start, node.End)))
		}
	case NodeEmph:
		w.Write([]byte("<em>"))
		r.renderChildren(w, idx, -1)
		w.Write([]byte("</em>"))
	case NodeStrong:
		w.Write([]byte("<strong>"))
		r.renderChildren(w, idx, -1)
		w.Write([]byte("</strong>"))
	case NodeSuperscript:
		w.Write([]byte("<sup>"))
		r.renderChildren(w, idx, -1)
		w.Write([]byte("</sup>"))
	case NodeSubscript:
		w.Write([]byte("<sub>"))
		r.renderChildren(w, idx, -1)
		w.Write([]byte("</sub>"))
	case NodeSpan:
		w.Write([]byte("<span"))
		r.renderAttributes(w, idx)
		w.Write([]byte(">"))
		r.renderChildren(w, idx, -1)
		w.Write([]byte("</span>"))
	case NodeMark:
		w.Write([]byte("<mark"))
		r.renderAttributes(w, idx)
		w.Write([]byte(">"))
		r.renderChildren(w, idx, -1)
		w.Write([]byte("</mark>"))
	case NodeInsert:
		w.Write([]byte("<ins"))
		r.renderAttributes(w, idx)
		w.Write([]byte(">"))
		r.renderChildren(w, idx, -1)
		w.Write([]byte("</ins>"))
	case NodeDelete:
		w.Write([]byte("<del"))
		r.renderAttributes(w, idx)
		w.Write([]byte(">"))
		r.renderChildren(w, idx, -1)
		w.Write([]byte("</del>"))
	case NodeDoubleQuoted:
		w.Write([]byte("“"))
		r.renderChildren(w, idx, -1)
		w.Write([]byte("”"))
	case NodeSingleQuoted:
		w.Write([]byte("‘"))
		r.renderChildren(w, idx, -1)
		w.Write([]byte("’"))
	case NodeLink:
		w.Write([]byte("<a"))
		urlIdx := r.findChildType(idx, NodeUrl)
		emailIdx := r.findChildType(idx, NodeEmail)
		if urlIdx != -1 {
			w.Write([]byte(" href=\""))
			urlBytes := r.getBytes(r.doc.Nodes[urlIdx].Start, r.doc.Nodes[urlIdx].End)
			urlBytes = bytes.ReplaceAll(urlBytes, []byte("\n"), nil)
			urlBytes = bytes.ReplaceAll(urlBytes, []byte("\r"), nil)
			w.Write(escapeHTML(unescapeString(urlBytes)))
			w.Write([]byte("\""))
		} else if emailIdx != -1 {
			w.Write([]byte(" href=\"mailto:"))
			urlBytes := r.getBytes(r.doc.Nodes[emailIdx].Start, r.doc.Nodes[emailIdx].End)
			urlBytes = bytes.ReplaceAll(urlBytes, []byte("\n"), nil)
			urlBytes = bytes.ReplaceAll(urlBytes, []byte("\r"), nil)
			w.Write(escapeHTML(unescapeString(urlBytes)))
			w.Write([]byte("\""))
		}
		r.renderAttributes(w, idx)
		w.Write([]byte(">"))
		r.renderChildren(w, idx, -1)
		w.Write([]byte("</a>"))
	case NodeImage:
		w.Write([]byte("<img"))
		urlIdx := r.findChildType(idx, NodeUrl)
		alt := r.getImageAltText(idx, urlIdx)
		w.Write([]byte(" alt=\""))
		w.Write(escapeHTML([]byte(alt)))
		w.Write([]byte("\""))
		if urlIdx != -1 {
			w.Write([]byte(" src=\""))
			urlBytes := r.getBytes(r.doc.Nodes[urlIdx].Start, r.doc.Nodes[urlIdx].End)
			urlBytes = bytes.ReplaceAll(urlBytes, []byte("\n"), nil)
			urlBytes = bytes.ReplaceAll(urlBytes, []byte("\r"), nil)
			w.Write(escapeHTML(unescapeString(urlBytes)))
			w.Write([]byte("\""))
		}
		r.renderAttributes(w, idx)
		w.Write([]byte(">"))
	case NodeSoftBreak:
		w.Write([]byte("\n"))
	case NodeHardBreak:
		w.Write([]byte("<br>\n"))
	case NodeNonBreakingSpace:
		w.Write([]byte("&nbsp;"))
	case NodeSmartPunctuation:
		content := r.getBytes(node.Start, node.End)
		if len(content) == 3 && content[0] == '-' && content[1] == '-' && content[2] == '-' {
			w.Write([]byte("—"))
		} else if len(content) == 2 && content[0] == '-' && content[1] == '-' {
			w.Write([]byte("–"))
		} else if len(content) == 3 && content[0] == '.' && content[1] == '.' && content[2] == '.' {
			w.Write([]byte("…"))
		} else if len(content) == 1 && content[0] == '\'' {
			w.Write([]byte("\u2019"))
		} else if len(content) == 1 && content[0] == '"' {
			w.Write([]byte("\u201C"))
		} else {
			w.Write(content)
		}
	case NodeSymb:
		w.Write([]byte(":"))
		w.Write(escapeHTMLText(r.getBytes(node.Start, node.End)))
		w.Write([]byte(":"))
	case NodeFootnoteReference:
		num := node.Data
		w.Write([]byte("<a id=\"fnref"))
		w.Write([]byte(strconv.Itoa(int(num))))
		w.Write([]byte("\" href=\"#fn"))
		w.Write([]byte(strconv.Itoa(int(num))))
		w.Write([]byte("\" role=\"doc-noteref\"><sup>"))
		w.Write([]byte(strconv.Itoa(int(num))))
		w.Write([]byte("</sup></a>"))
	}
	return nil
}

func (r *HTMLRenderer) renderChildren(w io.Writer, parentIdx int32, excludeIdx int32) {
	curr := r.doc.Nodes[parentIdx].Child
	for curr != -1 {
		if curr != excludeIdx {
			r.renderNode(w, curr)
		}
		curr = r.doc.Nodes[curr].Next
	}
}

func (r *HTMLRenderer) renderAttributes(w io.Writer, idx int32) {
	node := r.doc.Nodes[idx]
	if node.Attr == -1 {
		return
	}
	for j := uint16(0); j < node.AttrCount; j++ {
		attr := r.doc.Attributes[node.Attr+int32(j)]
		r.renderAttribute(w, attr)
	}
}

func (r *HTMLRenderer) renderAttribute(w io.Writer, attr Attribute) {
	if attr.KeyStart == -1 {
		w.Write([]byte(" id=\""))
		w.Write(escapeHTML(r.getAttrVal(attr)))
		w.Write([]byte("\""))
	} else if attr.KeyStart == -2 {
		w.Write([]byte(" class=\""))
		w.Write(escapeHTML(r.getAttrVal(attr)))
		w.Write([]byte("\""))
	} else if attr.KeyStart == -3 {
	} else {
		w.Write([]byte(" "))
		w.Write(r.doc.Source[attr.KeyStart : attr.KeyEnd+1])
		w.Write([]byte("=\""))
		w.Write(escapeHTML(r.getAttrVal(attr)))
		w.Write([]byte("\""))
	}
}

func (r *HTMLRenderer) getAttrVal(attr Attribute) []byte {
	if attr.ValStart < 0 {
		return r.doc.Extra[^attr.ValStart : attr.ValEnd+1]
	}
	return r.doc.Source[attr.ValStart : attr.ValEnd+1]
}

func (r *HTMLRenderer) findChildType(parentIdx int32, ntype NodeType) int32 {
	curr := r.doc.Nodes[parentIdx].Child
	for curr != -1 {
		if r.doc.Nodes[curr].Type == ntype {
			return curr
		}
		curr = r.doc.Nodes[curr].Next
	}
	return -1
}

func (r *HTMLRenderer) renderFootnotes(w io.Writer) error {
	if len(r.doc.UsedFootnotes) == 0 {
		return nil
	}
	w.Write([]byte("<section role=\"doc-endnotes\">\n<hr>\n<ol>\n"))
	for i, label := range r.doc.UsedFootnotes {
		num := i + 1
		numStr := strconv.Itoa(num)
		w.Write([]byte("<li id=\"fn" + numStr + "\">\n"))

		backlink := "<a href=\"#fnref" + numStr + "\" role=\"doc-backlink\">↩︎</a>"
		if contentIdx, ok := r.doc.FootnoteContent[label]; ok {
			var buf bytes.Buffer
			r.renderChildren(&buf, contentIdx, -1)
			body := buf.String()

			lastP := strings.LastIndex(body, "</p>")
			if lastP != -1 {
				body = body[:lastP] + backlink + body[lastP:]
			} else {
				body += "<p>" + backlink + "</p>\n"
			}
			w.Write([]byte(body))
		} else {
			w.Write([]byte("<p>" + backlink + "</p>\n"))
		}
		w.Write([]byte("</li>\n"))
	}
	w.Write([]byte("</ol>\n</section>\n"))
	return nil
}

func escapeHTML(b []byte) []byte {
	var res []byte
	for _, ch := range b {
		switch ch {
		case '&':
			res = append(res, []byte("&amp;")...)
		case '<':
			res = append(res, []byte("&lt;")...)
		case '>':
			res = append(res, []byte("&gt;")...)
		case '"':
			res = append(res, []byte("&quot;")...)
		default:
			res = append(res, ch)
		}
	}
	return res
}

func escapeHTMLText(b []byte) []byte {
	var res []byte
	for _, ch := range b {
		switch ch {
		case '&':
			res = append(res, []byte("&amp;")...)
		case '<':
			res = append(res, []byte("&lt;")...)
		case '>':
			res = append(res, []byte("&gt;")...)
		default:
			res = append(res, ch)
		}
	}
	return res
}

func unescapeString(b []byte) []byte {
	var res []byte
	escaped := false
	for _, ch := range b {
		if escaped {
			res = append(res, ch)
			escaped = false
		} else if ch == '\\' {
			escaped = true
		} else {
			res = append(res, ch)
		}
	}
	return res
}
