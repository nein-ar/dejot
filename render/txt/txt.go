package txt

import (
	"io"
	"log"
	"regexp"
	"strconv"
	"strings"

	"github.com/yashikota/mermaigo/pkg/mermaid"

	. "github.com/nein-ar/dejot/ast"
	"github.com/nein-ar/dejot/params"
)

const pageWidth = 74

var (
	sep  = strings.Repeat("=", pageWidth)
	sep2 = strings.Repeat("-", pageWidth)
)

func init() {
	params.RegisterRenderer("txt", []string{"indent"})
}

type TxtRenderer struct {
	doc        *Document
	h1Ctr      int
	h2Ctr      int
	h3Ctr      int
	level      int
	depthOver  int
	indentStep int
}

func NewTxtRenderer(doc *Document, p params.Params) *TxtRenderer {
	indentStep := 2
	if v := p.Get("indent", ""); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			indentStep = n
		}
	}
	for k := range p {
		if !params.SupportedBy("txt", k) {
			log.Printf("aspec/txt: unsupported param %q", k)
		}
	}
	return &TxtRenderer{
		doc:        doc,
		level:      1,
		depthOver:  -1,
		indentStep: indentStep,
	}
}

func (r *TxtRenderer) depth() int {
	if r.depthOver >= 0 {
		return r.depthOver
	}
	return r.level
}

func (r *TxtRenderer) listInd() string {
	return strings.Repeat(" ", (r.depth()+1)*r.indentStep)
}

func wrap(text string, width int, firstInd, restInd string) string {
	text = strings.TrimSpace(strings.Join(strings.Fields(text), " "))
	if text == "" {
		return ""
	}

	words := strings.Fields(text)
	var lines []string
	cur := ""

	for _, w := range words {
		ind := firstInd
		if len(lines) > 0 {
			ind = restInd
		}

		if cur == "" {
			cur = ind + w
		} else if len(cur)+1+len(w) <= width {
			cur += " " + w
		} else {
			lines = append(lines, cur)
			cur = restInd + w
		}
	}

	if cur != "" {
		lines = append(lines, cur)
	}

	return strings.Join(lines, "\n")
}

func isAlphaNum(ch byte) bool {
	return (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z')
}

func pad(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

func rtrim(s string) string {
	return strings.TrimRight(s, " ")
}

func (r *TxtRenderer) getBytes(node Node) []byte {
	if node.Start == -1 && node.End == -1 {
		return nil
	}
	if node.Start < 0 {
		idx := ^node.Start
		return r.doc.Extra[idx : node.End+1]
	}
	return r.doc.Source[node.Start : node.End+1]
}

func (r *TxtRenderer) getClasses(idx int32) []string {
	if idx < 0 || idx >= int32(len(r.doc.Nodes)) {
		return []string{}
	}
	node := r.doc.Nodes[idx]
	if node.Attr == -1 {
		return []string{}
	}

	var classes []string
	for j := uint16(0); j < node.AttrCount; j++ {
		attr := r.doc.Attributes[node.Attr+int32(j)]
		if attr.KeyStart == -2 {
			val := ""
			if attr.ValStart < 0 {
				val = string(r.doc.Extra[^attr.ValStart : attr.ValEnd+1])
			} else {
				val = string(r.doc.Source[attr.ValStart : attr.ValEnd+1])
			}
			for _, cls := range strings.Fields(val) {
				if cls != "" {
					classes = append(classes, cls)
				}
			}
		}
	}
	return classes
}

func (r *TxtRenderer) inlineText(idx int32) string {
	if idx < 0 || idx >= int32(len(r.doc.Nodes)) {
		return ""
	}

	node := r.doc.Nodes[idx]
	switch node.Type {
	case NodeStr:
		return string(r.getBytes(node))
	case NodeSoftBreak:
		return " "
	case NodeHardBreak:
		return "\n"
	case NodeNonBreakingSpace:
		return " "
	case NodeVerbatim:
		return string(r.getBytes(node))
	case NodeStrong:
		return strings.ToUpper(r.childrenText(idx))
	case NodeEmph:
		return "_" + r.childrenText(idx) + "_"
	case NodeLink:
		return r.childrenText(idx)
	case NodeImage:
		return ""
	case NodeFootnoteReference:
		num := int(node.Data)
		if num >= 0 && num < len(r.doc.UsedFootnotes) {
			return "[" + r.doc.UsedFootnotes[num] + "]"
		}
		return "[?]"
	case NodeRawInline:
		return string(r.getBytes(node))
	case NodeSmartPunctuation:
		content := string(r.getBytes(node))
		switch content {
		case "\u2013":
			return "-"
		case "\u2014":
			return "--"
		case "--":
			return "--"
		case "-":
			return "-"
		case "\u2026":
			return "..."
		case "...":
			return "..."
		case "\u201c":
			return "\""
		case "\u201d":
			return "\""
		case "\u2018":
			return "'"
		case "\u2019":
			return "'"
		case "'":
			return "'"
		case "\"":
			return "\""
		default:
			return content
		}
	case NodeDoubleQuoted:
		return "\"" + r.childrenText(idx) + "\""
	case NodeSingleQuoted:
		return "'" + r.childrenText(idx) + "'"
	default:
		return r.childrenText(idx)
	}
}

func (r *TxtRenderer) childrenText(idx int32) string {
	if idx < 0 || idx >= int32(len(r.doc.Nodes)) {
		return ""
	}

	node := r.doc.Nodes[idx]
	var result strings.Builder
	curr := node.Child
	for curr != -1 {
		result.WriteString(r.inlineText(curr))
		curr = r.doc.Nodes[curr].Next
	}
	return result.String()
}

func (r *TxtRenderer) para(idx int32) string {
	if idx < 0 || idx >= int32(len(r.doc.Nodes)) {
		return ""
	}

	node := r.doc.Nodes[idx]
	var result strings.Builder

	curr := node.Child
	for curr != -1 {
		result.WriteString(r.inlineText(curr))
		curr = r.doc.Nodes[curr].Next
	}

	text := strings.TrimSpace(result.String())

	if text == "" {
		return ""
	}

	ind := strings.Repeat(" ", r.depth()*r.indentStep)

	lines := strings.Split(text, "\n")
	var wrappedLines []string
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" && i > 0 && i < len(lines)-1 {
			wrappedLines = append(wrappedLines, "")
		} else if line != "" {
			wrapped := wrap(line, pageWidth, ind, ind)
			wrappedLines = append(wrappedLines, wrapped)
		}
	}
	return strings.Join(wrappedLines, "\n") + "\n\n"
}

func (r *TxtRenderer) getDepthOverride(idx int32) int {
	classes := r.getClasses(idx)
	depthRegex := regexp.MustCompile(`^depth-(\d+)$`)
	for _, cls := range classes {
		if match := depthRegex.FindStringSubmatch(cls); match != nil {
			if depth, err := strconv.Atoi(match[1]); err == nil {
				return depth
			}
		}
	}
	return -1
}

func (r *TxtRenderer) codeBlock(idx int32) string {
	if idx < 0 || idx >= int32(len(r.doc.Nodes)) {
		return ""
	}

	node := r.doc.Nodes[idx]
	classes := r.getClasses(idx)

	depthOverride := r.getDepthOverride(idx)

	lang := ""
	if len(classes) > 0 {
		lang = classes[0]
	}

	text := string(r.getBytes(node))

	switch lang {
	case "banner":
		ind := strings.Repeat(" ", r.depth()*r.indentStep)
		var bannerLines []string
		for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
			if line == "" {
				bannerLines = append(bannerLines, "")
			} else {
				bannerLines = append(bannerLines, ind+line)
			}
		}
		return strings.Join(bannerLines, "\n") + "\n\n"
	case "pointer":
		return r.renderPointer(text) + "\n\n"
	case "box":
		return r.renderBox(text) + "\n\n"
	case "exchange":
		return r.renderExchange(text) + "\n\n"
	case "abnf":
		ind := strings.Repeat(" ", r.depth()*r.indentStep)
		var abnfLines []string
		var lastEmpty bool
		for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
			isEmpty := strings.TrimSpace(line) == ""
			if isEmpty && lastEmpty {
				continue
			}
			if isEmpty {
				abnfLines = append(abnfLines, "")
			} else {
				abnfLines = append(abnfLines, ind+strings.TrimRight(line, " \t"))
			}
			lastEmpty = isEmpty
		}
		return strings.Join(abnfLines, "\n") + "\n"
	case "mermaid":
		result, err := mermaid.RenderText(text, &mermaid.TextRenderOptions{UseAscii: true})
		if err != nil {
			var lines []string
			for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
				lines = append(lines, "  "+line)
			}
			return "  [MERMAID -- render error: " + err.Error() + "]\n" + strings.Join(lines, "\n") + "\n"
		}
		var lines []string
		for _, line := range strings.Split(strings.TrimRight(result, "\n"), "\n") {
			if line == "" {
				lines = append(lines, "")
			} else {
				lines = append(lines, "  "+line)
			}
		}
		return strings.Join(lines, "\n") + "\n"
	default:
		depth := r.depth()
		if depthOverride >= 0 {
			depth = depthOverride
		}
		ind := strings.Repeat(" ", depth*r.indentStep)
		var lines []string
		var lastEmpty bool
		for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
			isEmpty := strings.TrimSpace(line) == ""
			if isEmpty && lastEmpty {
				continue
			}
			if isEmpty {
				lines = append(lines, "")
			} else {
				lines = append(lines, ind+strings.TrimRight(line, " \t"))
			}
			lastEmpty = isEmpty
		}
		return strings.Join(lines, "\n") + "\n"
	}
}

func (r *TxtRenderer) renderPointer(raw string) string {
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	if len(lines) < 2 {
		return raw
	}

	headerStr := lines[0]
	type mapping struct {
		cx    int
		label string
	}
	var allMappings []mapping

	for i := 1; i < len(lines); i++ {
		parts := strings.SplitN(lines[i], ":", 2)
		if len(parts) != 2 {
			continue
		}
		target := strings.TrimSpace(parts[0])
		label := strings.TrimSpace(parts[1])

		idx := -1
		if len(target) == 1 {
			isSpecial := !isAlphaNum(target[0])
			for j := 0; j <= len(headerStr)-len(target); j++ {
				if headerStr[j] == target[0] {
					before := j == 0 || headerStr[j-1] == ' '
					after := j+len(target) == len(headerStr) || headerStr[j+len(target)] == ' '
					if before && after {
						idx = j
						break
					}
				}
			}
			if idx < 0 && isSpecial {
				for j := 0; j <= len(headerStr)-len(target); j++ {
					if headerStr[j] == target[0] {
						afterOk := j+1 == len(headerStr) || headerStr[j+1] == ' '
						if afterOk {
							idx = j
							break
						}
					}
				}
			}
			if idx < 0 && isAlphaNum(target[0]) {
				for j := 0; j <= len(headerStr)-len(target); j++ {
					if headerStr[j] == target[0] {
						beforeOk := j == 0 || headerStr[j-1] == ' '
						afterOk := j+1 == len(headerStr) || headerStr[j+1] == ' '
						if beforeOk && afterOk {
							idx = j
							break
						}
					}
				}
			}
		} else {
			for j := 0; j <= len(headerStr)-len(target); j++ {
				if headerStr[j:j+len(target)] == target {
					before := j == 0 || headerStr[j-1] == ' '
					after := j+len(target) == len(headerStr) || headerStr[j+len(target)] == ' '
					if before && after {
						idx = j
						break
					}
				}
			}
		}
		if idx < 0 {
			continue
		}
		cx := idx + (len(target)-1)/2
		allMappings = append(allMappings, mapping{cx, label})
	}

	sortMappings := func(m []mapping) {
		for i := 0; i < len(m)-1; i++ {
			for j := i + 1; j < len(m); j++ {
				if m[j].cx < m[i].cx {
					m[i], m[j] = m[j], m[i]
				}
			}
		}
	}
	sortMappings(allMappings)

	if len(allMappings) == 0 {
		return headerStr
	}

	n := len(allMappings)
	mid := (n + 1) / 2
	leftMs := allMappings[:mid]
	rightMsSlice := allMappings[mid:]
	rightMs := make([]mapping, len(rightMsSlice))
	for i, m := range rightMsSlice {
		rightMs[len(rightMsSlice)-1-i] = m
	}

	nLeft := len(leftMs)
	nRight := len(rightMs)

	nSteps := make([]int, nLeft)
	for k := 0; k < nLeft; k++ {
		if k == 0 || k == nLeft-1 {
			nSteps[k] = 1
		} else {
			nSteps[k] = 2
		}
	}

	LPAD := 2
	for k := 0; k < nLeft; k++ {
		needPad := len(leftMs[k].label) + 2 - leftMs[k].cx + nSteps[k]
		if needPad > LPAD {
			LPAD = needPad
		}
	}

	lStart := make([]int, nLeft)
	lLabelRow := make([]int, nLeft)
	lLabelEnd := make([]int, nLeft)

	lStart[0] = 0
	if nLeft > 1 {
		lStart[1] = 0
	}

	occupied := make(map[int][]struct{ s, e int })

	collides := func(row, s, e int) bool {
		segs, ok := occupied[row]
		if !ok {
			return false
		}
		for _, seg := range segs {
			if s <= seg.e && e >= seg.s {
				return true
			}
		}
		return false
	}

	occupy := func(row, s, e int) {
		occupied[row] = append(occupied[row], struct{ s, e int }{s, e})
	}

	for k := 0; k < nLeft; k++ {
		if k >= 2 {
			lStart[k] = lLabelRow[k-2]
		}
		pcol := LPAD + leftMs[k].cx
		lastSlash := pcol - nSteps[k]
		labelEnd := lastSlash - 1
		labelLen := len(leftMs[k].label)
		labelSt := labelEnd - labelLen + 1
		labelRow := lStart[k] + nSteps[k]

		for collides(labelRow, labelSt, labelEnd) {
			labelRow++
			lStart[k] = labelRow - nSteps[k]
		}
		occupy(labelRow, labelSt, labelEnd)
		lLabelRow[k] = labelRow
		lLabelEnd[k] = labelEnd
	}

	maxRow := 1
	if nLeft > 0 && lLabelRow[nLeft-1]+1 > maxRow {
		maxRow = lLabelRow[nLeft-1] + 1
	}
	if nRight > 0 && nRight+1 > maxRow {
		maxRow = nRight + 1
	}

	grid := make([][]rune, maxRow)
	for r := 0; r < maxRow; r++ {
		grid[r] = make([]rune, pageWidth)
		for c := 0; c < pageWidth; c++ {
			grid[r][c] = ' '
		}
	}

	allCols := make([]int, len(allMappings))
	for k := 0; k < len(allMappings); k++ {
		allCols[k] = LPAD + allMappings[k].cx
	}

	retiredAt := make(map[int]int)
	for k := 0; k < nLeft; k++ {
		retiredAt[LPAD+leftMs[k].cx] = lStart[k]
	}
	for k := 0; k < nRight; k++ {
		retiredAt[LPAD+rightMs[k].cx] = k
	}

	drawPipes := func(row int) {
		for ki := 0; ki < len(allCols); ki++ {
			col := allCols[ki]
			if col < pageWidth {
				if retRow, ok := retiredAt[col]; !ok || retRow > row {
					grid[row][col] = '|'
				}
			}
		}
	}

	for k := 0; k < nLeft; k++ {
		pcol := LPAD + leftMs[k].cx
		for s := 1; s <= nSteps[k]; s++ {
			row := lStart[k] + s - 1
			if row >= maxRow {
				break
			}
			drawPipes(row)
			sc := pcol - s
			if sc >= 0 && sc < pageWidth {
				grid[row][sc] = '/'
			}
		}
		lr := lLabelRow[k]
		le := lLabelEnd[k]
		ls := le - len(leftMs[k].label) + 1
		if lr < maxRow {
			for c := 0; c < len(leftMs[k].label); c++ {
				gc := ls + c
				if gc >= 0 && gc < pageWidth {
					grid[lr][gc] = rune(leftMs[k].label[c])
				}
			}
		}
	}

	for k := 0; k < nRight; k++ {
		pcol := LPAD + rightMs[k].cx
		bsCol := pcol
		if k > 0 {
			bsCol = pcol + 1
		}
		bsRow := k
		lblRow := bsRow + 1
		lblCol := bsCol + 1

		if bsRow < maxRow {
			drawPipes(bsRow)
			if bsCol < pageWidth {
				grid[bsRow][bsCol] = '\\'
			}
		}
		if lblRow < maxRow {
			for c := 0; c < len(rightMs[k].label); c++ {
				gc := lblCol + c
				if gc < pageWidth {
					grid[lblRow][gc] = rune(rightMs[k].label[c])
				}
			}
		}
	}

	result := make([]string, maxRow+2)

	headerPadded := strings.Repeat(" ", LPAD) + headerStr
	result[0] = headerPadded

	headerLine := make([]rune, pageWidth)
	for i := 0; i < len(headerLine); i++ {
		headerLine[i] = ' '
	}
	for _, col := range allCols {
		if col >= 0 && col < pageWidth {
			headerLine[col] = '|'
		}
	}
	result[1] = rtrim(string(headerLine))

	for r := 0; r < maxRow; r++ {
		line := string(grid[r])
		result[r+2] = rtrim(line)
	}
	return strings.Join(result, "\n")
}

func (r *TxtRenderer) renderBox(raw string) string {
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	type item struct {
		label string
		depth int
	}
	var parsed []item
	for _, l := range lines {
		trimmed := strings.TrimLeft(l, " ")
		spaceCount := len(l) - len(trimmed)
		depth := spaceCount / 2

		if strings.HasPrefix(trimmed, "[") && strings.Contains(trimmed, "]") {
			end := strings.Index(trimmed, "]")
			label := trimmed[1:end]
			parsed = append(parsed, item{label, depth})
		} else {
			parsed = append(parsed, item{strings.TrimSpace(l), 0})
		}
	}

	var out []string
	for i := 0; i < len(parsed); i++ {
		d := parsed[i].depth
		label := parsed[i].label
		var pre1, pre2 strings.Builder

		for dv := 0; dv < d; dv++ {
			hasSib := false
			for j := i + 1; j < len(parsed); j++ {
				if parsed[j].depth < dv {
					break
				}
				if parsed[j].depth == dv {
					hasSib = true
					break
				}
			}
			if dv == d-1 {
				if hasSib {
					pre1.WriteString("|-- ")
				} else {
					pre1.WriteString("`-- ")
				}
			} else {
				if hasSib {
					pre1.WriteString("|   ")
				} else {
					pre1.WriteString("    ")
				}
			}
			if hasSib {
				pre2.WriteString("|   ")
			} else {
				pre2.WriteString("    ")
			}
		}

		bw := len(label) + 2
		out = append(out, pre2.String()+"+"+strings.Repeat("-", bw)+"+")
		out = append(out, pre1.String()+"| "+label+" |")
		out = append(out, pre2.String()+"+"+strings.Repeat("-", bw)+"+")

		if i < len(parsed)-1 {
			var stem strings.Builder
			for dv := 0; dv < parsed[i+1].depth; dv++ {
				hasSib := false
				for j := i + 1; j < len(parsed); j++ {
					if parsed[j].depth < dv {
						break
					}
					if parsed[j].depth == dv {
						hasSib = true
						break
					}
				}
				if hasSib {
					stem.WriteString("|   ")
				} else {
					stem.WriteString("    ")
				}
			}
			out = append(out, rtrim(stem.String()))
		}
	}
	return strings.Join(out, "\n")
}

func (r *TxtRenderer) renderExchange(raw string) string {
	lines := strings.Split(strings.TrimRight(raw, "\n"), "\n")
	if len(lines) == 0 {
		return ""
	}

	leftActor := "CLIENT"
	rightActor := "SERVER"
	startIdx := 0

	firstLine := strings.TrimSpace(lines[0])
	lBracket := strings.Index(firstLine, "[")
	rBracket := strings.Index(firstLine, "]")
	if lBracket >= 0 && rBracket > lBracket {
		leftPart := firstLine[lBracket+1 : rBracket]
		rest := strings.TrimSpace(firstLine[rBracket+1:])
		if strings.HasPrefix(rest, "[") {
			rBracket2 := strings.Index(rest, "]")
			if rBracket2 > 0 {
				rightPart := rest[1:rBracket2]
				leftActor = strings.ToUpper(strings.TrimSpace(leftPart))
				rightActor = strings.ToUpper(strings.TrimSpace(rightPart))
				startIdx = 1
			}
		}
	}

	const LCOL = 24
	const ARROW = 10
	ARROW_R := " " + strings.Repeat("-", ARROW-3) + "> "
	ARROW_L := " <" + strings.Repeat("-", ARROW-3) + " "

	wrapTo := func(text string, width int) []string {
		if len(text) <= width {
			return []string{text}
		}
		words := strings.Fields(text)
		var out []string
		var cur string
		for _, w := range words {
			if cur == "" {
				cur = w
			} else if len(cur)+1+len(w) <= width {
				cur += " " + w
			} else {
				out = append(out, cur)
				cur = w
			}
		}
		if cur != "" {
			out = append(out, cur)
		}
		return out
	}

	var rows []string

	rows = append(rows, pad(leftActor, LCOL+ARROW)+rightActor)
	rows = append(rows, pad(strings.Repeat("-", len(leftActor)), LCOL+ARROW)+strings.Repeat("-", len(rightActor)))
	rows = append(rows, "")

	side := "L"
	var msgBuf []string

	flush := func() {
		if len(msgBuf) == 0 {
			return
		}
		text := strings.TrimSpace(strings.Join(msgBuf, " "))
		msgBuf = nil
		wrapped := wrapTo(text, LCOL)

		if side == "L" {
			for i := 0; i < len(wrapped)-1; i++ {
				rows = append(rows, pad(wrapped[i], LCOL))
			}
			rows = append(rows, pad(wrapped[len(wrapped)-1], LCOL)+ARROW_R)
		} else {
			rows = append(rows, strings.Repeat(" ", LCOL)+ARROW_L+wrapped[0])
			cont := strings.Repeat(" ", LCOL+ARROW)
			for i := 1; i < len(wrapped); i++ {
				rows = append(rows, cont+wrapped[i])
			}
		}
	}

	for i := startIdx; i < len(lines); i++ {
		l := lines[i]
		t := strings.TrimSpace(l)
		if t == "" || t == "---" {
			flush()
			rows = append(rows, "")
			continue
		}
		if len(t) > 0 && t[0] == '>' {
			flush()
			side = "L"
			msg := t[1:]
			if len(msg) > 0 && msg[0] == ' ' {
				msg = msg[1:]
			}
			msgBuf = []string{msg}
		} else if len(t) > 0 && t[0] == '<' {
			flush()
			side = "R"
			msg := t[1:]
			if len(msg) > 0 && msg[0] == ' ' {
				msg = msg[1:]
			}
			msgBuf = []string{msg}
		} else {
			trimmed := l
			for j := 0; j < 6 && len(trimmed) > 0 && trimmed[0] == ' '; j++ {
				trimmed = trimmed[1:]
			}
			msgBuf = append(msgBuf, trimmed)
		}
	}
	flush()

	var result strings.Builder
	for _, row := range rows {
		tr := rtrim(row)
		if tr != "" {
			result.WriteString("      " + tr + "\n")
		} else {
			result.WriteString("\n")
		}
	}
	return strings.TrimRight(result.String(), "\n")
}

func (r *TxtRenderer) blockquote(idx int32) string {
	if idx < 0 || idx >= int32(len(r.doc.Nodes)) {
		return ""
	}

	classes := r.getClasses(idx)
	label := "NOTE"
	for _, cls := range classes {
		if cls == "note" || cls == "warn" || cls == "warning" || cls == "important" || cls == "rationale" || cls == "aside" || cls == "quote" {
			label = strings.ToUpper(cls)
			break
		}
	}

	if label == "WARN" || label == "WARNING" {
		label = "WARN"
	}

	text := r.childrenText(idx)
	return r.renderAdmonition(label, text, r.listInd())
}

func (r *TxtRenderer) renderAdmonition(label, text, ind string) string {
	lbl := "[" + label + "]"
	firstInd := ind + lbl + "  "
	restInd := ind + strings.Repeat(" ", len(lbl)+2)
	return wrap(text, pageWidth, firstInd, restInd) + "\n"
}

func (r *TxtRenderer) div(idx int32) string {
	if idx < 0 || idx >= int32(len(r.doc.Nodes)) {
		return ""
	}

	node := r.doc.Nodes[idx]
	classes := r.getClasses(idx)

	for _, cls := range classes {
		if cls == "note" || cls == "warn" || cls == "warning" || cls == "important" || cls == "rationale" || cls == "aside" || cls == "quote" {
			label := strings.ToUpper(cls)
			if label == "WARNING" {
				label = "WARN"
			}
			text := r.childrenText(idx)
			return r.renderAdmonition(label, text, r.listInd())
		}
	}

	for _, cls := range classes {
		if strings.HasPrefix(cls, "depth-") {
			depthStr := strings.TrimPrefix(cls, "depth-")
			if depthNum, err := strconv.Atoi(depthStr); err == nil {
				oldDepth := r.depthOver
				r.depthOver = depthNum
				if r.depthOver > 4 {
					r.depthOver = 4
				}
				var result strings.Builder
				curr := node.Child
				for curr != -1 {
					result.WriteString(r.renderNode(curr))
					curr = r.doc.Nodes[curr].Next
				}
				r.depthOver = oldDepth
				return result.String()
			}
		}
	}

	var result strings.Builder
	curr := node.Child
	for curr != -1 {
		result.WriteString(r.renderNode(curr))
		curr = r.doc.Nodes[curr].Next
	}
	return result.String()
}

func (r *TxtRenderer) list(idx int32) string {
	if idx < 0 || idx >= int32(len(r.doc.Nodes)) {
		return ""
	}

	node := r.doc.Nodes[idx]
	ordered := node.Type == NodeOrderedList
	ind := r.listInd()

	startNum := 1
	if node.Data>>16 > 0 {
		startNum = int((node.Data >> 16) & 0xFFFF)
	}

	itemNum := startNum
	var result strings.Builder

	curr := node.Child
	for curr != -1 {
		if r.doc.Nodes[curr].Type == NodeListItem || r.doc.Nodes[curr].Type == NodeTaskListItem {
			marker := "-"
			if ordered {
				marker = strconv.Itoa(itemNum) + "."
			}

			itemNode := r.doc.Nodes[curr]
			itemCurr := itemNode.Child
			for itemCurr != -1 {
				childNode := r.doc.Nodes[itemCurr]
				if childNode.Type == NodePara {
					text := r.childrenText(itemCurr)
					text = strings.TrimSpace(text)
					firstInd := ind + marker + " "
					restInd := ind + strings.Repeat(" ", len(marker)+1)
					result.WriteString(wrap(text, pageWidth, firstInd, restInd) + "\n")
				} else {
					oldDepth := r.depthOver
					r.depthOver = 4
					result.WriteString(r.renderNode(itemCurr))
					r.depthOver = oldDepth
				}
				itemCurr = childNode.Next
			}
			itemNum++
		} else {
			result.WriteString(r.renderNode(curr))
		}
		curr = r.doc.Nodes[curr].Next
	}

	return result.String()
}

func (r *TxtRenderer) table(idx int32) string {
	if idx < 0 || idx >= int32(len(r.doc.Nodes)) {
		return ""
	}

	node := r.doc.Nodes[idx]
	var headers []string
	var rows [][]string

	rowIdx := node.Child
	firstRow := true

	for rowIdx != -1 {
		if r.doc.Nodes[rowIdx].Type == NodeRow {
			var row []string
			cellIdx := r.doc.Nodes[rowIdx].Child

			for cellIdx != -1 {
				text := r.childrenText(cellIdx)
				text = strings.TrimSpace(text)
				row = append(row, text)
				cellIdx = r.doc.Nodes[cellIdx].Next
			}

			if firstRow {
				headers = row
				firstRow = false
			} else {
				rows = append(rows, row)
			}
		}
		rowIdx = r.doc.Nodes[rowIdx].Next
	}

	return r.renderTable(headers, rows, "  ") + "\n\n"
}

func (r *TxtRenderer) renderTable(headers []string, rows [][]string, indent string) string {
	if len(headers) == 0 {
		return ""
	}

	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}

	for _, row := range rows {
		for i, cell := range row {
			if i < len(widths) && len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}

	var result strings.Builder
	result.WriteString(indent)

	for i, h := range headers {
		if i > 0 {
			result.WriteString("  |  ")
		}
		result.WriteString(pad(h, widths[i]))
	}
	result.WriteString("\n")
	result.WriteString(indent)

	for i := range headers {
		if i > 0 {
			result.WriteString("--+--")
		}
		result.WriteString(strings.Repeat("-", widths[i]))
	}
	result.WriteString("\n")

	for _, row := range rows {
		result.WriteString(indent)
		for i, cell := range row {
			if i > 0 {
				result.WriteString("  |  ")
			}
			if i < len(widths) {
				result.WriteString(pad(cell, widths[i]))
			} else {
				result.WriteString(cell)
			}
		}
		result.WriteString("\n")
	}

	lines := strings.Split(strings.TrimRight(result.String(), "\n"), "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " \t")
	}
	return strings.Join(lines, "\n")
}

func (r *TxtRenderer) thematicBreak(idx int32) string {
	if idx < 0 || idx >= int32(len(r.doc.Nodes)) {
		return ""
	}

	classes := r.getClasses(idx)
	isMajor := false
	for _, cls := range classes {
		if cls == "major" {
			isMajor = true
			break
		}
	}

	if isMajor {
		return "\n" + sep + "\n\n"
	}
	return "\n" + sep2 + "\n\n"
}

func (r *TxtRenderer) footnotesBlock() string {
	if len(r.doc.UsedFootnotes) == 0 {
		return ""
	}

	var result strings.Builder
	result.WriteString("\n" + sep + "\nFOOTNOTES\n" + sep + "\n\n")

	for i, label := range r.doc.UsedFootnotes {
		num := i + 1
		if contentIdx, ok := r.doc.FootnoteContent[label]; ok && contentIdx >= 0 && contentIdx < int32(len(r.doc.Nodes)) {
			text := r.childrenText(contentIdx)
			text = strings.TrimSpace(text)
			numStr := strconv.Itoa(num)
			wrapped := wrap(text, pageWidth, "  ["+numStr+"]  ", "  "+strings.Repeat(" ", len(numStr))+"  ")
			result.WriteString(wrapped + "\n")
		}
	}

	return result.String()
}

func (r *TxtRenderer) defList(idx int32) string {
	if idx < 0 || idx >= int32(len(r.doc.Nodes)) {
		return ""
	}

	node := r.doc.Nodes[idx]
	var items []struct {
		term string
		defs []string
	}
	var maxTermLen int

	curr := node.Child
	for curr != -1 {
		if r.doc.Nodes[curr].Type == NodeDefinitionListItem {
			itemNode := r.doc.Nodes[curr]
			var term string
			var defs []string

			itemCurr := itemNode.Child
			for itemCurr != -1 {
				childNode := r.doc.Nodes[itemCurr]
				switch childNode.Type {
				case NodeTerm:
					term = r.childrenText(itemCurr)
					term = strings.TrimSpace(term)
					if len(term) > maxTermLen {
						maxTermLen = len(term)
					}
				case NodeDefinition:
					def := r.childrenText(itemCurr)
					def = strings.TrimSpace(def)
					defs = append(defs, def)
				}
				itemCurr = childNode.Next
			}
			items = append(items, struct {
				term string
				defs []string
			}{term, defs})
		}
		curr = r.doc.Nodes[curr].Next
	}

	var result strings.Builder
	for _, item := range items {
		for _, def := range item.defs {
			term := item.term
			for len(term) < maxTermLen {
				term += " "
			}
			firstInd := "    " + term + " : "
			restInd := "    " + strings.Repeat(" ", maxTermLen+3)
			result.WriteString(wrap(def, pageWidth, firstInd, restInd) + "\n")
		}
	}

	return result.String()
}

func (r *TxtRenderer) buildTOC() string {
	type heading struct {
		level int16
		text  string
	}
	var heads []heading

	var walk func(int32)
	firstH1 := true
	walk = func(idx int32) {
		for idx != -1 {
			if idx >= int32(len(r.doc.Nodes)) {
				break
			}
			node := r.doc.Nodes[idx]
			if node.Type == NodeHeading && (node.Level == 1 || node.Level == 2) {
				text := r.childrenText(idx)
				text = strings.TrimSpace(text)
				if node.Level == 1 {
					if firstH1 && !strings.HasPrefix(strings.ToUpper(text), "SECTION") {
						firstH1 = false
					} else {
						heads = append(heads, heading{node.Level, text})
						firstH1 = false
					}
				} else {
					heads = append(heads, heading{node.Level, text})
				}
			}
			if node.Child != -1 {
				walk(node.Child)
			}
			idx = node.Next
		}
	}
	walk(r.doc.Nodes[0].Child)

	if len(heads) == 0 {
		return ""
	}

	var items []string
	items = append(items, "  TABLE OF CONTENTS")
	items = append(items, "  "+strings.Repeat("-", pageWidth-4))

	h1c, h2c := 0, 0
	for _, h := range heads {
		if h.level == 1 {
			h1c++
			h2c = 0
			title := h.text
			if len(title) > pageWidth-6 {
				title = title[:pageWidth-9] + "..."
			}
			title = strings.ToUpper(title)
			dotsLen := pageWidth - 2 - len(title) - 1
			if dotsLen < 2 {
				dotsLen = 2
			}
			items = append(items, "  "+title+" "+strings.Repeat(".", dotsLen))
		} else {
			h2c++
			label := strconv.Itoa(h1c) + "." + strconv.Itoa(h2c) + "  " + h.text
			title := label
			if len(title) > pageWidth-8 {
				title = title[:pageWidth-11] + "..."
			}
			dotsLen := pageWidth - 4 - len(title) - 1
			if dotsLen < 2 {
				dotsLen = 2
			}
			items = append(items, "    "+title+" "+strings.Repeat(".", dotsLen))
		}
	}

	return strings.Join(items, "\n") + "\n"
}

func (r *TxtRenderer) renderNode(idx int32) string {
	if idx < 0 || idx >= int32(len(r.doc.Nodes)) {
		return ""
	}

	node := r.doc.Nodes[idx]

	switch node.Type {
	case NodePara:
		return r.para(idx)
	case NodeHeading:
		text := r.childrenText(idx)
		text = strings.TrimSpace(text)

		var result strings.Builder

		if node.Level == 1 {
			prefix := ""
			if r.h1Ctr > 0 {
				prefix = "\n"
			}
			result.WriteString(prefix + sep + "\n" + text + "\n" + sep + "\n\n")

			sectionRegex := regexp.MustCompile(`^SECTION\s+(\d+)`)
			if match := sectionRegex.FindStringSubmatch(text); match != nil {
				if secNum, err := strconv.Atoi(match[1]); err == nil {
					r.h1Ctr = secNum
				} else {
					r.h1Ctr++
				}
			} else {
				r.h1Ctr++
			}
			r.h2Ctr = 0
			r.h3Ctr = 0
			r.level = 1
			r.depthOver = -1

		} else if node.Level == 2 {
			label := text
			numRegex := regexp.MustCompile(`^(\d+\.\d+[a-z]?)\s+(.*)$`)
			if match := numRegex.FindStringSubmatch(text); match != nil {
				label = "  " + match[1] + "  " + match[2]
			} else {
				label = "  " + strconv.Itoa(r.h1Ctr) + "." + strconv.Itoa(r.h2Ctr) + "  " + text
				r.h2Ctr++
			}
			r.h3Ctr = 0
			r.level = 2
			r.depthOver = -1

			result.WriteString("\n" + label + "\n\n")

		} else if node.Level == 3 {
			label := "    " + strconv.Itoa(r.h1Ctr) + "." + strconv.Itoa(r.h2Ctr) + "." + strconv.Itoa(r.h3Ctr) + "  " + text
			r.h3Ctr++
			r.level = 3
			r.depthOver = -1
			result.WriteString("\n" + label + "\n\n")

		} else {
			result.WriteString("\n  " + text + "\n")
		}

		curr := node.Child
		for curr != -1 {
			result.WriteString(r.renderNode(curr))
			curr = r.doc.Nodes[curr].Next
		}

		return result.String()

	case NodeThematicBreak:
		return r.thematicBreak(idx)
	case NodeDiv:
		return r.div(idx)
	case NodeCodeBlock:
		return r.codeBlock(idx)
	case NodeBlockQuote:
		return r.blockquote(idx)
	case NodeBulletList, NodeOrderedList, NodeTaskList:
		return r.list(idx)
	case NodeDefinitionList:
		return r.defList(idx)
	case NodeTable:
		return r.table(idx)
	case NodeListItem:
		var result strings.Builder
		curr := node.Child
		var text strings.Builder
		for curr != -1 {
			text.WriteString(r.inlineText(curr))
			curr = r.doc.Nodes[curr].Next
		}
		textStr := strings.TrimSpace(text.String())
		result.WriteString(wrap(textStr, pageWidth, "    - ", "      ") + "\n")
		return result.String()
	case NodeFootnote, NodeReference:
		return ""
	case NodeDoc:
		var result strings.Builder
		curr := node.Child
		for curr != -1 {
			result.WriteString(r.renderNode(curr))
			curr = r.doc.Nodes[curr].Next
		}
		return result.String()
	default:
		var result strings.Builder
		curr := node.Child
		for curr != -1 {
			result.WriteString(r.renderNode(curr))
			curr = r.doc.Nodes[curr].Next
		}
		return result.String()
	}
}

func (r *TxtRenderer) Render(w io.Writer) error {
	body := r.renderNode(0)
	body += r.footnotesBlock()

	for strings.Contains(body, "\n\n\n") {
		body = strings.ReplaceAll(body, "\n\n\n", "\n\n")
	}

	body = strings.TrimRight(body, " \t\n") + "\n"
	_, err := io.WriteString(w, body)
	return err
}
