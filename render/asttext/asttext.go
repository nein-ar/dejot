// Package asttext renders the internal AST in the same human-readable form as
// the reference implementation's renderAST. It is used by the functional test
// suite and by debugging tools.
package asttext

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	. "github.com/nein-ar/dejot/ast"
)

type renderer struct {
	doc *Document
	pos bool
	buf strings.Builder
}

// Render returns the indented AST text. Positions are included when pos is true.
func Render(doc *Document, pos bool) string {
	r := &renderer{doc: doc, pos: pos}
	if len(doc.Nodes) == 0 {
		return ""
	}
	r.node(0, 0)
	r.references()
	r.footnotes()
	return r.buf.String()
}

func (r *renderer) node(idx int32, indent int) {
	if idx < 0 || int(idx) >= len(r.doc.Nodes) {
		return
	}
	n := r.doc.Nodes[idx]
	if skipNode(n.Type) {
		return
	}
	r.buf.WriteString(strings.Repeat(" ", indent))
	r.buf.WriteString(tag(n.Type))
	if r.pos && n.Type != NodeDoc {
		if pos := r.position(n); pos != "" {
			r.buf.WriteString(pos)
		}
	}
	r.fields(n)
	r.attributes(n)
	r.buf.WriteByte('\n')

	if n.Type == NodeTable {
		for child := n.Child; child != -1; child = r.doc.Nodes[child].Next {
			if r.doc.Nodes[child].Type == NodeCaption {
				r.node(child, indent+2)
			}
		}
	}
	for child := n.Child; child != -1; child = r.doc.Nodes[child].Next {
		if n.Type == NodeTable && r.doc.Nodes[child].Type == NodeCaption {
			continue
		}
		r.node(child, indent+2)
	}
}

func skipNode(t NodeType) bool {
	switch t {
	case NodeReference, NodeFootnote, NodeUrl, NodeEmail:
		return true
	}
	return false
}

func (r *renderer) fields(n Node) {
	switch n.Type {
	case NodeHeading:
		level := n.Level
		if level == 0 {
			level = int16(n.Data & 0xFFFF)
		}
		r.fieldInt("level", int64(level))
	case NodeBulletList, NodeTaskList:
		r.fieldBool("tight", n.Data&DataListTight != 0)
		if n.Type == NodeBulletList {
			r.fieldString("style", markerAt(r.doc.Source, n.Start))
		}
	case NodeOrderedList:
		r.fieldBool("tight", n.Data&DataListTight != 0)
		if style := orderedStyle(r.doc.Source, n.Start); style != "" {
			r.fieldString("style", style)
		}
		if start := int(n.Data >> 16); start != 0 && start != 1 {
			r.fieldInt("start", int64(start))
		}
	case NodeRow:
		r.fieldBool("head", r.rowIsHead(n))
	case NodeCell:
		r.fieldBool("head", n.Data&DataCellHeader != 0)
		r.fieldString("align", cellAlign(n))
	case NodeStr:
		r.fieldString("text", r.text(n))
	case NodeSymb:
		r.fieldString("alias", strings.Trim(r.text(n), ":"))
	case NodeVerbatim, NodeInlineMath, NodeDisplayMath:
		r.fieldString("text", r.text(n))
	case NodeRawInline:
		r.fieldString("format", r.format(n))
		r.fieldString("text", r.text(n))
	case NodeCodeBlock:
		if lang := r.class(n); lang != "" {
			r.fieldString("lang", lang)
		}
		r.fieldString("text", r.text(n))
	case NodeRawBlock:
		r.fieldString("format", r.format(n))
		r.fieldString("text", r.text(n))
	case NodeLink, NodeImage:
		if dest := r.destination(n); dest != "" {
			r.fieldString("destination", dest)
		}
	case NodeFootnoteReference:
		num := int(n.Data)
		if num > 0 && num <= len(r.doc.UsedFootnotes) {
			r.fieldString("text", r.doc.UsedFootnotes[num-1])
		}
	case NodeSmartPunctuation:
		text := r.text(n)
		r.fieldString("type", smartType(text))
		r.fieldString("text", text)
	case NodeTaskListItem:
		status := "unchecked"
		if n.Data&DataTaskChecked != 0 {
			status = "checked"
		}
		r.fieldString("checkbox", status)
	}
}

func (r *renderer) attributes(n Node) {
	if n.Attr == -1 {
		return
	}
	for j := uint16(0); j < n.AttrCount; j++ {
		attr := r.doc.Attributes[n.Attr+int32(j)]
		if attr.KeyStart == AttrKeyFormat {
			continue
		}
		key := ""
		switch attr.KeyStart {
		case AttrKeyID:
			key = "id"
		case AttrKeyClass:
			key = "class"
		default:
			if attr.KeyStart >= 0 {
				key = string(r.doc.Source[attr.KeyStart : attr.KeyEnd+1])
			}
		}
		if key == "" {
			continue
		}
		r.buf.WriteByte(' ')
		r.buf.WriteString(key)
		r.buf.WriteByte('=')
		r.buf.WriteString(quote(r.attrValue(attr)))
	}
}

func (r *renderer) references() {
	if len(r.doc.References) == 0 {
		return
	}
	keys := make([]string, 0, len(r.doc.References))
	for k := range r.doc.References {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	r.buf.WriteString("references\n")
	for _, k := range keys {
		ref := r.doc.References[k]
		r.buf.WriteString("  [" + quote(k) + "] =\n")
		r.buf.WriteString("    reference label=" + quote(k) + " destination=" + quote(r.refValue(ref)))
		r.buf.WriteByte('\n')
	}
}

func (r *renderer) footnotes() {
	if len(r.doc.FootnoteContent) == 0 {
		return
	}
	keys := make([]string, 0, len(r.doc.FootnoteContent))
	for k := range r.doc.FootnoteContent {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	r.buf.WriteString("footnotes\n")
	for _, k := range keys {
		idx := r.doc.FootnoteContent[k]
		if idx < 0 || int(idx) >= len(r.doc.Nodes) {
			continue
		}
		r.buf.WriteString("  [" + quote(k) + "] =\n")
		r.buf.WriteString("    footnote label=" + quote(k) + "\n")
		for child := r.doc.Nodes[idx].Child; child != -1; child = r.doc.Nodes[child].Next {
			r.node(child, 6)
		}
	}
}

func (r *renderer) fieldString(key, value string) {
	r.buf.WriteString(" " + key + "=" + quote(value))
}

func (r *renderer) fieldBool(key string, value bool) {
	r.buf.WriteString(" " + key + "=" + fmt.Sprint(value))
}

func (r *renderer) fieldInt(key string, value int64) {
	r.buf.WriteString(" " + key + "=" + fmt.Sprint(value))
}

func (r *renderer) text(n Node) string {
	return string(r.bytes(n))
}

func (r *renderer) bytes(n Node) []byte {
	if n.Start == -1 && n.End == -1 {
		return nil
	}
	if n.Start < 0 {
		idx := ^n.Start
		if idx < 0 || n.End < idx || int(n.End) >= len(r.doc.Extra) {
			return nil
		}
		return r.doc.Extra[idx : n.End+1]
	}
	if n.Start < 0 || n.End < n.Start || int(n.End) >= len(r.doc.Source) {
		return nil
	}
	return r.doc.Source[n.Start : n.End+1]
}

func (r *renderer) attrValue(a Attribute) string {
	if a.ValStart < 0 {
		idx := ^a.ValStart
		if idx < 0 || a.ValEnd < idx || int(a.ValEnd) >= len(r.doc.Extra) {
			return ""
		}
		return normalizeAttrValue(string(r.doc.Extra[idx : a.ValEnd+1]))
	}
	if a.ValStart < 0 || a.ValEnd < a.ValStart || int(a.ValEnd) >= len(r.doc.Source) {
		return ""
	}
	return normalizeAttrValue(string(r.doc.Source[a.ValStart : a.ValEnd+1]))
}

func (r *renderer) class(n Node) string {
	if n.Attr == -1 {
		return ""
	}
	for j := uint16(0); j < n.AttrCount; j++ {
		attr := r.doc.Attributes[n.Attr+int32(j)]
		if attr.KeyStart == AttrKeyClass {
			return r.attrValue(attr)
		}
	}
	return ""
}

func (r *renderer) format(n Node) string {
	if n.Attr == -1 {
		return ""
	}
	for j := uint16(0); j < n.AttrCount; j++ {
		attr := r.doc.Attributes[n.Attr+int32(j)]
		if attr.KeyStart == AttrKeyFormat {
			return strings.TrimPrefix(r.attrValue(attr), "=")
		}
	}
	return ""
}

func (r *renderer) destination(n Node) string {
	for child := n.Child; child != -1; child = r.doc.Nodes[child].Next {
		c := r.doc.Nodes[child]
		if c.Type == NodeUrl || c.Type == NodeEmail {
			return r.text(c)
		}
	}
	return ""
}

func (r *renderer) rowIsHead(row Node) bool {
	head := false
	hasCell := false
	for child := row.Child; child != -1; child = r.doc.Nodes[child].Next {
		cell := r.doc.Nodes[child]
		if cell.Type != NodeCell {
			continue
		}
		hasCell = true
		if cell.Data&DataCellHeader == 0 {
			return false
		}
		head = true
	}
	return hasCell && head
}

func (r *renderer) refValue(ref Reference) string {
	if ref.DestStart < 0 {
		idx := ^ref.DestStart
		if idx < 0 || ref.DestEnd < idx || int(ref.DestEnd) >= len(r.doc.Extra) {
			return ""
		}
		return string(r.doc.Extra[idx : ref.DestEnd+1])
	}
	if ref.DestStart < 0 || ref.DestEnd < ref.DestStart || int(ref.DestEnd) >= len(r.doc.Source) {
		return ""
	}
	return string(r.doc.Source[ref.DestStart : ref.DestEnd+1])
}

func (r *renderer) position(n Node) string {
	if n.Start < 0 || n.End < n.Start {
		return ""
	}
	startLine, startCol, startOff := sourceLoc(r.doc.Source, n.Start)
	endLine, endCol, endOff := sourceLoc(r.doc.Source, n.End)
	return fmt.Sprintf(" (%d:%d:%d-%d:%d:%d)", startLine, startCol, startOff, endLine, endCol, endOff)
}

func sourceLoc(source []byte, offset int32) (line, col, off int) {
	if offset < 0 {
		return 0, 0, 0
	}
	line, col = 1, 1
	limit := int(offset)
	if limit > len(source) {
		limit = len(source)
	}
	for i := 0; i < limit; i++ {
		if source[i] == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	if int(offset) < len(source) && source[offset] == '\n' {
		line++
		col = 0
	}
	return line, col, int(offset)
}

func tag(t NodeType) string {
	switch t {
	case NodeDoc:
		return "doc"
	case NodeSection:
		return "section"
	case NodePara:
		return "para"
	case NodeHeading:
		return "heading"
	case NodeThematicBreak:
		return "thematic_break"
	case NodeDiv:
		return "div"
	case NodeCodeBlock:
		return "code_block"
	case NodeRawBlock:
		return "raw_block"
	case NodeBlockQuote:
		return "block_quote"
	case NodeOrderedList:
		return "ordered_list"
	case NodeBulletList:
		return "bullet_list"
	case NodeTaskList:
		return "task_list"
	case NodeDefinitionList:
		return "definition_list"
	case NodeTable:
		return "table"
	case NodeCaption:
		return "caption"
	case NodeRow:
		return "row"
	case NodeCell:
		return "cell"
	case NodeListItem:
		return "list_item"
	case NodeTaskListItem:
		return "task_list_item"
	case NodeDefinitionListItem:
		return "definition_list_item"
	case NodeTerm:
		return "term"
	case NodeDefinition:
		return "definition"
	case NodeFootnote:
		return "footnote"
	case NodeReference:
		return "reference"
	case NodeStr:
		return "str"
	case NodeSoftBreak:
		return "soft_break"
	case NodeHardBreak:
		return "hard_break"
	case NodeNonBreakingSpace:
		return "non_breaking_space"
	case NodeSymb:
		return "symb"
	case NodeVerbatim:
		return "verbatim"
	case NodeRawInline:
		return "raw_inline"
	case NodeInlineMath:
		return "inline_math"
	case NodeDisplayMath:
		return "display_math"
	case NodeUrl:
		return "url"
	case NodeEmail:
		return "email"
	case NodeFootnoteReference:
		return "footnote_reference"
	case NodeSmartPunctuation:
		return "smart_punctuation"
	case NodeEmph:
		return "emph"
	case NodeStrong:
		return "strong"
	case NodeLink:
		return "link"
	case NodeImage:
		return "image"
	case NodeSpan:
		return "span"
	case NodeMark:
		return "mark"
	case NodeSuperscript:
		return "superscript"
	case NodeSubscript:
		return "subscript"
	case NodeInsert:
		return "insert"
	case NodeDelete:
		return "delete"
	case NodeDoubleQuoted:
		return "double_quoted"
	case NodeSingleQuoted:
		return "single_quoted"
	}
	return "unknown"
}

func markerAt(source []byte, start int32) string {
	if start < 0 || int(start) >= len(source) {
		return ""
	}
	return string(source[start])
}

func orderedStyle(source []byte, start int32) string {
	if start < 0 || int(start) >= len(source) {
		return ""
	}
	i := start
	for i < int32(len(source)) && (isDigit(source[i]) || isAlpha(source[i]) || source[i] == '(') {
		i++
	}
	if i < int32(len(source)) && (source[i] == '.' || source[i] == ')') {
		return string(source[start : i+1])
	}
	return ""
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }
func isAlpha(b byte) bool { return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') }

func cellAlign(n Node) string {
	switch {
	case n.Data&DataAlignCenter != 0:
		return "center"
	case n.Data&DataAlignLeft != 0:
		return "left"
	case n.Data&DataAlignRight != 0:
		return "right"
	}
	return "default"
}

func smartType(text string) string {
	switch text {
	case "...":
		return "ellipses"
	case "--":
		return "en_dash"
	case "---":
		return "em_dash"
	case "'":
		return "right_single_quote"
	case `"`:
		return "left_double_quote"
	}
	return "unknown"
}

func normalizeAttrValue(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
			b.WriteByte(s[i])
			continue
		}
		b.WriteByte(s[i])
	}
	var parts []string
	for _, line := range strings.Split(b.String(), "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, ">")
		parts = append(parts, strings.Fields(line)...)
	}
	return strings.Join(parts, " ")
}

func quote(s string) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimSuffix(b.String(), "\n")
}
