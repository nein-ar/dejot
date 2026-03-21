package parser

import (
	"bytes"
	"strconv"
	"strings"

	. "github.com/nein-ar/dejot/ast"
)

type Assembler struct {
	doc   *Document
	stack []int32

	pendingAttr  []Attribute
	sectionStack []int16

	references     map[string]Reference
	lastClosedNode int32

	footnoteLabels  map[string]int
	usedFootnotes   []string
	footnoteContent map[string]int32

	usedIDs    map[string]bool
	headingIDs map[int32]string

	currentTableAlignments []uint32
}

func (a *Assembler) Assemble(events []Event) {
	a.doc.Nodes = make([]Node, 0, len(events)/2+8)
	a.doc.Attributes = make([]Attribute, 0, 16)
	a.doc.References = make(map[string]Reference)
	a.doc.Extra = make([]byte, 0, 64)
	a.stack = make([]int32, 0, 16)
	a.pendingAttr = make([]Attribute, 0, 8)
	a.sectionStack = make([]int16, 0, 8)
	a.references = make(map[string]Reference)
	a.lastClosedNode = -1
	a.footnoteLabels = make(map[string]int)
	a.usedFootnotes = make([]string, 0, 8)
	a.footnoteContent = make(map[string]int32)
	a.usedIDs = make(map[string]bool)
	a.headingIDs = make(map[int32]string)

	a.collectReferences(events)
	a.collectHeadingReferences(events)

	rootIdx := a.addNode(NodeDoc, 0, 0)
	a.stack = append(a.stack, rootIdx)

	type listTightState struct {
		nodeIdx    int32
		blanklines bool
	}
	var listTightStack []listTightState

	i := 0
	for i < len(events) {
		ev := events[i]

		if len(listTightStack) > 0 {
			top := &listTightStack[len(listTightStack)-1]
			switch ev.Type {
			case EvBlankLine:
				top.blanklines = true
			case EvOpenList, EvCloseList:
				top.blanklines = false
			case EvOpenListItem, EvCloseListItem:
			default:
				if top.blanklines {
					a.doc.Nodes[top.nodeIdx].Data &= ^DataListTight
				}
				top.blanklines = false
			}
		}

		switch ev.Type {
		case EvOpenAttributes:
			i = a.parseAttributes(events, i)
		case EvOpenInlineAttributes:
			i = a.parseAttributes(events, i)
			if a.lastClosedNode != -1 && len(a.pendingAttr) > 0 {
				if a.doc.Nodes[a.lastClosedNode].Type == NodeStr {
					nodeIdx := a.lastClosedNode
					content := a.doc.Source[a.doc.Nodes[nodeIdx].Start : a.doc.Nodes[nodeIdx].End+1]
					if len(content) == 0 || isSpace(content[len(content)-1]) {
						a.pendingAttr = a.pendingAttr[:0]
					} else {
						lastWordStart := -1
						for j := len(content) - 1; j >= 0; j-- {
							if isSpace(content[j]) {
								lastWordStart = j + 1
								break
							}
						}
						if lastWordStart == -1 {
							lastWordStart = 0
						}

						if lastWordStart > 0 {
							origStart := a.doc.Nodes[nodeIdx].Start
							origEnd := a.doc.Nodes[nodeIdx].End
							newEnd := origStart + int32(lastWordStart) - 1

							spanIdx := a.addNode(NodeSpan, origStart+int32(lastWordStart), origEnd)
							wordIdx := a.addNode(NodeStr, origStart+int32(lastWordStart), origEnd)

							a.doc.Nodes[nodeIdx].End = newEnd
							a.doc.Nodes[spanIdx].Child = wordIdx

							parentIdx := a.stack[len(a.stack)-1]
							a.linkToParent(parentIdx, spanIdx)

							a.lastClosedNode = spanIdx
						} else if lastWordStart == 0 {
							origStart := a.doc.Nodes[nodeIdx].Start
							origEnd := a.doc.Nodes[nodeIdx].End
							childIdx := a.addNode(NodeStr, origStart, origEnd)
							a.doc.Nodes[nodeIdx].Type = NodeSpan
							a.doc.Nodes[nodeIdx].Child = childIdx
						}
					}
				}
				a.applyPendingAttr(a.lastClosedNode)
			} else {
				a.pendingAttr = a.pendingAttr[:0]
			}
		case EvOpenPara:
			idx := a.openNode(NodePara, ev.Start)
			a.applyPendingAttr(idx)
		case EvClosePara:
			a.lastClosedNode = a.closeNode(ev.End)
		case EvOpenHeading:
			level := int16(ev.End - ev.Start + 1)
			a.handleHeadingOpen(level, ev.Start)
		case EvCloseHeading:
			idx := a.closeNode(ev.End)
			a.generateID(idx, a.doc.Nodes[idx].Start)
			a.lastClosedNode = idx
		case EvOpenBlockQuote:
			idx := a.openNode(NodeBlockQuote, ev.Start)
			a.applyPendingAttr(idx)
		case EvCloseBlockQuote:
			a.lastClosedNode = a.closeNode(ev.End)
		case EvOpenList:
			marker := byte(ev.End)
			ntype := NodeBulletList
			if isOrderedMarker(marker) {
				ntype = NodeOrderedList
			} else if marker == ':' {
				ntype = NodeDefinitionList
			}
			idx := a.openNode(ntype, ev.Start)
			a.applyPendingAttr(idx)
			a.doc.Nodes[idx].Data |= DataListTight
			a.setStyleFromMarker(idx, marker)
			listTightStack = append(listTightStack, listTightState{nodeIdx: idx})

			if ntype == NodeOrderedList && i+1 < len(events) && events[i+1].Type == EvNone {
				i++
				startNum := int(events[i].End)
				if startNum != 1 {
					a.doc.Nodes[idx].Data |= uint32(startNum) << 16
				}
			}
		case EvCloseList:
			a.lastClosedNode = a.closeNode(ev.Start)
			if len(listTightStack) > 0 {
				listTightStack = listTightStack[:len(listTightStack)-1]
			}
		case EvOpenListItem:
			pIdx := a.stack[len(a.stack)-1]
			ntype := NodeListItem
			if a.doc.Nodes[pIdx].Type == NodeDefinitionList {
				ntype = NodeDefinitionListItem
			}
			idx := a.openNode(ntype, ev.Start)
			a.applyPendingAttr(idx)
			if ev.End > 0 && ntype == NodeListItem {
				a.doc.Nodes[idx].Type = NodeTaskListItem
				if ev.End == 2 {
					a.doc.Nodes[idx].Data |= DataTaskChecked
				}
				pIdx := a.stack[len(a.stack)-2]
				if a.doc.Nodes[pIdx].Type == NodeBulletList {
					a.doc.Nodes[pIdx].Type = NodeTaskList
				}
			}
		case EvCloseListItem:
			idx := a.stack[len(a.stack)-1]
			if a.doc.Nodes[idx].Type == NodeDefinitionListItem {
				a.handleDefinitionListItem(idx)
			}
			a.lastClosedNode = a.closeNode(ev.End)
		case EvOpenCodeBlock, EvOpenRawBlock:
			ntype := NodeCodeBlock
			closeType := EvCloseCodeBlock
			if ev.Type == EvOpenRawBlock {
				ntype = NodeRawBlock
				closeType = EvCloseRawBlock
			}
			idx := a.openNode(ntype, ev.Start)
			a.applyPendingAttr(idx)

			j := i + 1
			contentStart := int32(-1)
			foundStartMarker := false
			for j < len(events) {
				if !foundStartMarker && events[j].Type == EvNone {
					foundStartMarker = true
				} else if events[j].Type == EvStr {
					if contentStart == -1 {
						contentStart = events[j].Start
					}
				} else if events[j].Type == closeType {
					break
				}
				j++
			}

			if contentStart != -1 {
				var buf bytes.Buffer
				for k := i + 1; k < j; k++ {
					if events[k].Type == EvStr {
						buf.Write(a.doc.Source[events[k].Start : events[k].End+1])
					}
				}
				extraStart := int32(len(a.doc.Extra))
				a.doc.Extra = append(a.doc.Extra, buf.Bytes()...)
				a.doc.Nodes[idx].Start = ^extraStart
				a.doc.Nodes[idx].End = int32(len(a.doc.Extra)) - 1
			} else {
				a.doc.Nodes[idx].Start = -1
				a.doc.Nodes[idx].End = -1
			}

			if ntype == NodeRawBlock && idx != -1 {
				node := &a.doc.Nodes[idx]
				if node.Attr != -1 {
					for k := uint16(0); k < node.AttrCount; k++ {
						attr := &a.doc.Attributes[node.Attr+int32(k)]
						if attr.KeyStart == -2 {
							val := string(a.getAttrVal(*attr))
							if strings.HasPrefix(val, "=") {
								attr.KeyStart = -3
							}
						}
					}
				}
			}
			a.closeNode(ev.End)
			i = j
		case EvOpenDiv:
			idx := a.openNode(NodeDiv, ev.Start)
			a.applyPendingAttr(idx)
		case EvCloseDiv:
			a.lastClosedNode = a.closeNode(ev.End)
		case EvOpenTable:
			idx := a.openNode(NodeTable, ev.Start)
			a.applyPendingAttr(idx)
		case EvCloseTable:
			a.lastClosedNode = a.closeNode(ev.End)
		case EvOpenCaption:
			idx := int32(-1)
			if a.lastClosedNode != -1 && a.doc.Nodes[a.lastClosedNode].Type == NodeTable {
				tableIdx := a.lastClosedNode
				captionIdx := a.addNode(NodeCaption, ev.Start, -1)
				a.linkToParent(tableIdx, captionIdx)
				a.stack = append(a.stack, captionIdx)
				a.lastClosedNode = -1
				idx = captionIdx
			} else {
				idx = a.openNode(NodeCaption, ev.Start)
			}
			a.applyPendingAttr(idx)
		case EvCloseCaption:
			a.lastClosedNode = a.closeNode(ev.End)
		case EvOpenRow:
			a.openNode(NodeRow, ev.Start)
		case EvCloseRow:
			a.lastClosedNode = a.closeNode(ev.End)
		case EvOpenCell:
			idx := a.openNode(NodeCell, ev.Start)
			a.applyPendingAttr(idx)

			col := 0
			curr := a.doc.Nodes[a.stack[len(a.stack)-2]].Child
			for curr != -1 && curr != idx {
				curr = a.doc.Nodes[curr].Next
				col++
			}
			if col < len(a.currentTableAlignments) {
				a.doc.Nodes[idx].Data &= ^(DataAlignLeft | DataAlignCenter | DataAlignRight)
				a.doc.Nodes[idx].Data |= a.currentTableAlignments[col]
			}
		case EvCloseCell:
			a.lastClosedNode = a.closeNode(ev.End)
		case EvOpenFootnote:
			j := i + 1
			label := ""
			if j < len(events) && events[j].Type == EvStr {
				content := a.doc.Source[events[j].Start : events[j].End+1]
				if len(content) > 0 && content[0] == '[' && content[1] == '^' {
					closeIdx := bytes.IndexByte(content, ']')
					if closeIdx != -1 {
						label = normalizeLabel(string(content[2:closeIdx]))
					}
				}
			}
			idx := a.openNode(NodeFootnote, ev.Start)
			if label != "" {
				a.footnoteContent[label] = idx
			}
			a.applyPendingAttr(idx)
		case EvCloseFootnote:
			a.lastClosedNode = a.closeNode(ev.End)
		case EvOpenReferenceDefinition:
			idx := a.openNode(NodeReference, ev.Start)
			a.applyPendingAttr(idx)
		case EvCloseReferenceDefinition:
			a.closeNode(ev.End)
		case EvOpenLinkText:
			idx := a.openNode(NodeLink, ev.Start)
			a.applyPendingAttr(idx)
		case EvCloseLinkText:
			a.lastClosedNode = a.closeNode(ev.End)
		case EvOpenImageText:
			idx := a.openNode(NodeImage, ev.Start)
			a.applyPendingAttr(idx)
		case EvCloseImageText:
			a.lastClosedNode = a.closeNode(ev.End)
		case EvOpenSpan:
			idx := a.openNode(NodeSpan, ev.Start)
			a.applyPendingAttr(idx)
		case EvCloseSpan:
			a.lastClosedNode = a.closeNode(ev.End)
		case EvOpenDoubleQuoted:
			a.openNode(NodeDoubleQuoted, ev.Start)
		case EvCloseDoubleQuoted:
			a.lastClosedNode = a.closeNode(ev.End)
		case EvOpenSingleQuoted:
			a.openNode(NodeSingleQuoted, ev.Start)
		case EvCloseSingleQuoted:
			a.lastClosedNode = a.closeNode(ev.End)
		case EvOpenMark:
			idx := a.openNode(NodeMark, ev.Start)
			a.applyPendingAttr(idx)
		case EvCloseMark:
			a.lastClosedNode = a.closeNode(ev.End)
		case EvOpenInsert:
			idx := a.openNode(NodeInsert, ev.Start)
			a.applyPendingAttr(idx)
		case EvCloseInsert:
			a.lastClosedNode = a.closeNode(ev.End)
		case EvOpenDelete:
			idx := a.openNode(NodeDelete, ev.Start)
			a.applyPendingAttr(idx)
		case EvCloseDelete:
			a.lastClosedNode = a.closeNode(ev.End)
		case EvOpenDestination:
			a.addLeafNode(NodeUrl, ev.Start, ev.End)
		case EvUrl, EvEmail:
			idx := a.openNode(NodeLink, ev.Start)
			a.applyPendingAttr(idx)

			destType := NodeUrl
			if ev.Type == EvEmail {
				destType = NodeEmail
			}
			destIdx := a.addNode(destType, ev.Start, ev.End)
			a.linkToParent(idx, destIdx)

			strIdx := a.addNode(NodeStr, ev.Start, ev.End)
			a.linkToParent(idx, strIdx)

			a.lastClosedNode = a.closeNode(ev.End)
		case EvOpenReference:
			label := string(a.doc.Source[ev.Start : ev.End+1])
			if label == "" {
				var buf strings.Builder
				a.collectPlainText(a.stack[len(a.stack)-1], &buf)
				label = buf.String()
			}
			label = normalizeLabel(label)
			if ref, ok := a.references[label]; ok {
				a.addLeafNode(NodeUrl, ref.DestStart, ref.DestEnd)
				if len(ref.Attributes) > 0 {
					parentIdx := a.stack[len(a.stack)-1]
					a.pendingAttr = append(a.pendingAttr, ref.Attributes...)
					a.applyPendingAttr(parentIdx)
				}
			}
		case EvFootnoteReference:
			label := string(a.doc.Source[ev.Start+2 : ev.End])
			num, ok := a.footnoteLabels[label]
			if !ok {
				num = len(a.usedFootnotes) + 1
				a.footnoteLabels[label] = num
				a.usedFootnotes = append(a.usedFootnotes, label)
			}
			idx := a.addNode(NodeFootnoteReference, ev.Start, ev.End)
			a.doc.Nodes[idx].Data = uint32(num)
			parentIdx := a.stack[len(a.stack)-1]
			a.linkToParent(parentIdx, idx)
		case EvSmartPunctuation:
			a.addLeafNode(NodeSmartPunctuation, ev.Start, ev.End)
		case EvInlineMath:
			a.addLeafNode(NodeInlineMath, ev.Start, ev.End)
		case EvDisplayMath:
			a.addLeafNode(NodeDisplayMath, ev.Start, ev.End)
		case EvRawInline:
			idx := a.addNode(NodeRawInline, ev.Start, ev.End)
			a.linkToParent(a.stack[len(a.stack)-1], idx)
			a.lastClosedNode = idx
			i++
			for i < len(events) && events[i].Type != EvRawInline {
				i++
			}
		case EvOpenVerbatim:
			a.openNode(NodeVerbatim, ev.Start)
		case EvCloseVerbatim:
			a.lastClosedNode = a.closeNode(ev.End)
		case EvSoftBreak:
			a.addLeafNode(NodeSoftBreak, ev.Start, ev.End)
		case EvHardBreak:
			a.addLeafNode(NodeHardBreak, ev.Start, ev.End)
		case EvNonBreakingSpace:
			a.addLeafNode(NodeNonBreakingSpace, ev.Start, ev.End)
		case EvThematicBreak:
			idx := a.addNode(NodeThematicBreak, ev.Start, ev.End)
			a.linkToParent(a.stack[len(a.stack)-1], idx)
			a.applyPendingAttr(idx)
		case EvBlankLine:
		case EvStr:
			skipThisStr := false
			if i > 0 && events[i-1].Type == EvOpenFootnote {
				content := a.doc.Source[ev.Start : ev.End+1]
				if len(content) > 0 && content[0] == '[' && len(content) > 1 && content[1] == '^' {
					if bytes.Contains(content, []byte("]:")) {
						skipThisStr = true
					}
				}
			}
			if !skipThisStr {
				tipIdx := a.stack[len(a.stack)-1]
				if a.doc.Nodes[tipIdx].Type == NodeTable {
					a.handleTableSeparator(ev.Start, ev.End)
				} else {
					a.addLeafNode(NodeStr, ev.Start, ev.End)
				}
			}
		case EvSymb:
			a.addLeafNode(NodeSymb, ev.Start, ev.End)
		case EvOpenEmph:
			idx := a.openNode(NodeEmph, ev.Start)
			a.applyPendingAttr(idx)
		case EvCloseEmph:
			a.lastClosedNode = a.closeNode(ev.End)
		case EvOpenStrong:
			idx := a.openNode(NodeStrong, ev.Start)
			a.applyPendingAttr(idx)
		case EvCloseStrong:
			a.lastClosedNode = a.closeNode(ev.End)
		case EvEscape:
			a.addLeafNode(NodeStr, ev.Start+1, ev.End)
		case EvOpenSuperscript:
			a.openNode(NodeSuperscript, ev.Start)
		case EvCloseSuperscript:
			a.lastClosedNode = a.closeNode(ev.End)
		case EvOpenSubscript:
			a.openNode(NodeSubscript, ev.Start)
		case EvCloseSubscript:
			a.lastClosedNode = a.closeNode(ev.End)
		}
		i++
	}

	for _, label := range a.usedFootnotes {
		if contentIdx, ok := a.footnoteContent[label]; ok {
			a.linkToParent(rootIdx, contentIdx)
		}
	}

	a.doc.UsedFootnotes = a.usedFootnotes
	a.doc.FootnoteContent = a.footnoteContent
	a.doc.References = a.references

	if len(a.doc.Source) > 0 {
		a.doc.Nodes[rootIdx].End = int32(len(a.doc.Source) - 1)
	}
}

func normalizeLabel(label string) string {
	return strings.Join(strings.Fields(label), " ")
}

func unescapeBytes(b []byte) []byte {
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

func (a *Assembler) collectReferences(events []Event) {
	var pendingAttr []Attribute
	for i := 0; i < len(events); i++ {
		ev := events[i]
		switch ev.Type {
		case EvOpenAttributes, EvOpenInlineAttributes:
			i = a.parseAttributesTo(events, i, &pendingAttr)
			continue
		case EvOpenPara, EvOpenBlockQuote, EvOpenList, EvOpenListItem, EvOpenCodeBlock, EvOpenDiv, EvOpenTable, EvOpenCaption, EvOpenRow, EvOpenCell, EvOpenHeading:
			pendingAttr = pendingAttr[:0]
		case EvOpenReferenceDefinition:
			var content string
			j := i + 1
			for j < len(events) && events[j].Type != EvCloseReferenceDefinition {
				if events[j].Type == EvStr || events[j].Type == EvText {
					content = string(a.doc.Source[events[j].Start : events[j].End+1])
				}
				j++
			}

			if content == "" {
				lineStart := events[i].Start
				lineEnd := lineStart
				for lineEnd < int32(len(a.doc.Source)) && a.doc.Source[lineEnd] != '\n' {
					lineEnd++
				}
				if lineEnd > lineStart {
					content = string(a.doc.Source[lineStart:lineEnd])
				}
			}

			if len(content) > 0 && content[0] == '[' {
				closeIdx := strings.IndexByte(content, ']')
				if closeIdx != -1 && closeIdx+1 < len(content) && content[closeIdx+1] == ':' {
					label := normalizeLabel(content[1:closeIdx])
					value := content[closeIdx+2:]

					value = strings.ReplaceAll(value, "\r\n", "\n")
					value = strings.ReplaceAll(value, "\r", "\n")
					parts := strings.Split(value, "\n")
					var cleanedParts []string
					for _, p := range parts {
						cleaned := strings.TrimSpace(p)
						if len(cleaned) > 0 {
							cleanedParts = append(cleanedParts, cleaned)
						}
					}
					cleanValue := strings.Join(cleanedParts, "")
					valStart := int32(len(a.doc.Extra))
					a.doc.Extra = append(a.doc.Extra, []byte(cleanValue)...)
					valEnd := int32(len(a.doc.Extra)) - 1

					refAttrs := make([]Attribute, len(pendingAttr))
					copy(refAttrs, pendingAttr)

					a.references[label] = Reference{
						DestStart:  ^valStart,
						DestEnd:    valEnd,
						Attributes: refAttrs,
					}
					pendingAttr = pendingAttr[:0]
				}
			}
			i = j
		case EvOpenFootnote:
			j := i + 1
			if j < len(events) && events[j].Type == EvStr {
				content := a.doc.Source[events[j].Start : events[j].End+1]
				if len(content) > 0 && content[0] == '[' && content[1] == '^' {
					closeIdx := bytes.IndexByte(content, ']')
					if closeIdx != -1 && closeIdx+1 < len(content) && content[closeIdx+1] == ':' {
						label := normalizeLabel(string(content[2:closeIdx]))
						a.footnoteContent[label] = -2
					}
				}
			}
			pendingAttr = pendingAttr[:0]
		}
	}
}

func (a *Assembler) parseAttributesTo(events []Event, startIdx int, target *[]Attribute) int {
	i := startIdx + 1
	for i < len(events) && events[i].Type != EvCloseAttributes {
		ev := events[i]
		switch ev.Type {
		case EvAttrIdMarker:
			*target = append(*target, Attribute{
				KeyStart: -1,
				ValStart: ev.Start,
				ValEnd:   ev.End,
			})
		case EvAttrClassMarker:
			*target = append(*target, Attribute{
				KeyStart: -2,
				ValStart: ev.Start,
				ValEnd:   ev.End,
			})
		case EvAttrKey:
			keyEv := ev
			if keyEv.Start < int32(len(a.doc.Source)) && a.doc.Source[keyEv.Start] == '=' {
				*target = append(*target, Attribute{
					KeyStart: -3,
					ValStart: keyEv.Start + 1,
					ValEnd:   keyEv.End,
				})
			} else {
				for i+1 < len(events) && events[i+1].Type != EvCloseAttributes {
					i++
					if events[i].Type == EvAttrValue {
						*target = append(*target, Attribute{
							KeyStart: keyEv.Start,
							KeyEnd:   keyEv.End,
							ValStart: events[i].Start,
							ValEnd:   events[i].End,
						})
						break
					}
				}
			}
		}
		i++
	}
	return i
}

func (a *Assembler) collectHeadingReferences(events []Event) {
	tempAs := &Assembler{doc: a.doc, references: a.references, usedIDs: make(map[string]bool)}
	i := 0
	for i < len(events) {
		ev := events[i]
		switch ev.Type {
		case EvOpenAttributes:
			i = tempAs.parseAttributes(events, i)
			continue
		case EvOpenPara, EvOpenBlockQuote, EvOpenList, EvOpenListItem, EvOpenCodeBlock, EvOpenDiv, EvOpenTable, EvOpenCaption, EvOpenRow, EvOpenCell:
			if len(tempAs.pendingAttr) > 0 {
				for _, attr := range tempAs.pendingAttr {
					if attr.KeyStart == -1 {
						var id string
						if attr.ValStart < 0 {
							id = string(a.doc.Extra[^attr.ValStart : attr.ValEnd+1])
						} else {
							id = string(a.doc.Source[attr.ValStart : attr.ValEnd+1])
						}
						tempAs.usedIDs[id] = true
						break
					}
				}
				tempAs.pendingAttr = tempAs.pendingAttr[:0]
			}
		case EvOpenHeading:
			headingStart := ev.Start
			j := i + 1
			var buf strings.Builder
			for j < len(events) && events[j].Type != EvCloseHeading {
				if events[j].Type == EvStr {
					buf.Write(a.doc.Source[events[j].Start : events[j].End+1])
				} else if events[j].Type == EvSoftBreak {
					buf.WriteByte('\n')
				}
				j++
			}
			text := buf.String()
			label := normalizeLabel(text)

			var manualID string
			if len(tempAs.pendingAttr) > 0 {
				for _, attr := range tempAs.pendingAttr {
					if attr.KeyStart == -1 {
						if attr.ValStart < 0 {
							manualID = string(a.doc.Extra[^attr.ValStart : attr.ValEnd+1])
						} else {
							manualID = string(a.doc.Source[attr.ValStart : attr.ValEnd+1])
						}
						tempAs.usedIDs[manualID] = true
						break
					}
				}
				tempAs.pendingAttr = tempAs.pendingAttr[:0]
			}

			ident := manualID
			if ident == "" {
				base := a.slugify(text)
				ident = base
				if ident == "" {
					ident = "s"
				}
				if tempAs.usedIDs[ident] || base == "" {
					k := 1
					if base == "" {
						ident = "s-1"
					} else {
						ident = base + "-1"
					}
					for tempAs.usedIDs[ident] {
						k++
						ident = base + "-" + strconv.Itoa(k)
						if base == "" {
							ident = "s-" + strconv.Itoa(k)
						}
					}
				}
				tempAs.usedIDs[ident] = true
			}

			a.headingIDs[headingStart] = ident

			if _, ok := a.references[label]; !ok {
				start := int32(len(a.doc.Extra))
				a.doc.Extra = append(a.doc.Extra, '#')
				a.doc.Extra = append(a.doc.Extra, []byte(ident)...)
				a.references[label] = Reference{
					DestStart: ^start,
					DestEnd:   int32(len(a.doc.Extra)) - 1,
				}
			}
			i = j
		}
		i++
	}
}

func (a *Assembler) generateID(idx int32, start int32) {
	node := &a.doc.Nodes[idx]
	if node.Data&DataNoAutoID != 0 {
		return
	}

	parentIdx := a.stack[len(a.stack)-1]
	targetIdx := idx
	if a.doc.Nodes[parentIdx].Type == NodeSection {
		targetIdx = parentIdx
	}

	if a.doc.Nodes[targetIdx].Attr != -1 {
		for j := uint16(0); j < a.doc.Nodes[targetIdx].AttrCount; j++ {
			attr := a.doc.Attributes[a.doc.Nodes[targetIdx].Attr+int32(j)]
			if attr.KeyStart == -1 {
				return
			}
		}
	}

	ident, ok := a.headingIDs[start]
	if !ok {
		return
	}

	valStart := int32(len(a.doc.Extra))
	a.doc.Extra = append(a.doc.Extra, []byte(ident)...)
	valEnd := int32(len(a.doc.Extra)) - 1

	attr := Attribute{
		KeyStart: -1,
		ValStart: ^valStart,
		ValEnd:   valEnd,
	}

	a.pendingAttr = append(a.pendingAttr, attr)
	a.applyPendingAttr(targetIdx)
}

func (a *Assembler) collectPlainText(idx int32, buf *strings.Builder) {
	curr := a.doc.Nodes[idx].Child
	for curr != -1 {
		node := a.doc.Nodes[curr]
		if node.Type == NodeStr {
			buf.Write(a.doc.Source[node.Start : node.End+1])
		} else if node.Type == NodeSoftBreak {
			buf.WriteByte('\n')
		} else {
			a.collectPlainText(curr, buf)
		}
		curr = a.doc.Nodes[curr].Next
	}
}

func (a *Assembler) slugify(text string) string {
	var res []rune
	lastWasSpec := false
	for _, r := range text {
		isSpec := strings.ContainsRune("][~!@#$%^&*(){}`,.<>\\|=+/? \t\r\n", r)
		if isSpec {
			if !lastWasSpec {
				res = append(res, ' ')
			}
			lastWasSpec = true
		} else {
			res = append(res, r)
			lastWasSpec = false
		}
	}
	s := strings.TrimSpace(string(res))
	return strings.ReplaceAll(s, " ", "-")
}

func (a *Assembler) handleTableSeparator(start, end int32) {
	parentIdx := a.stack[len(a.stack)-1]
	curr := a.doc.Nodes[parentIdx].Child
	var lastRow int32 = -1
	for curr != -1 {
		if a.doc.Nodes[curr].Type == NodeRow {
			lastRow = curr
		}
		curr = a.doc.Nodes[curr].Next
	}

	line := a.doc.Source[start : end+1]
	parts := bytes.Split(line, []byte("|"))

	newAlignments := make([]uint32, 0)
	for _, part := range parts {
		part = bytes.TrimSpace(part)
		if len(part) == 0 {
			continue
		}

		align := uint32(0)
		left := part[0] == ':'
		right := part[len(part)-1] == ':'

		if left && right {
			align = DataAlignCenter
		} else if left {
			align = DataAlignLeft
		} else if right {
			align = DataAlignRight
		}
		newAlignments = append(newAlignments, align)
	}
	a.currentTableAlignments = newAlignments

	if lastRow != -1 {
		cell := a.doc.Nodes[lastRow].Child
		col := 0
		for cell != -1 {
			a.doc.Nodes[cell].Data |= DataCellHeader
			if col < len(a.currentTableAlignments) {
				a.doc.Nodes[cell].Data &= ^(DataAlignLeft | DataAlignCenter | DataAlignRight)
				a.doc.Nodes[cell].Data |= a.currentTableAlignments[col]
			}
			cell = a.doc.Nodes[cell].Next
			col++
		}
	}
}

func (a *Assembler) handleHeadingOpen(level int16, start int32) {
	for len(a.sectionStack) > 0 && a.sectionStack[len(a.sectionStack)-1] >= level {
		a.stack = a.stack[:len(a.stack)-1]
		a.sectionStack = a.sectionStack[:len(a.sectionStack)-1]
	}

	parentIdx := a.stack[len(a.stack)-1]
	parentType := a.doc.Nodes[parentIdx].Type

	if parentType == NodeDoc || parentType == NodeSection {
		sIdx := a.openNode(NodeSection, start)
		a.sectionStack = append(a.sectionStack, level)
		a.applyPendingAttr(sIdx)
	}

	hIdx := a.openNode(NodeHeading, start)
	a.doc.Nodes[hIdx].Level = level
	if len(a.pendingAttr) > 0 {
		a.doc.Nodes[hIdx].Data |= DataNoAutoID
		a.applyPendingAttr(hIdx)
	}
}

func (a *Assembler) parseAttributes(events []Event, startIdx int) int {
	i := startIdx + 1
	for i < len(events) && events[i].Type != EvCloseAttributes {
		ev := events[i]
		switch ev.Type {
		case EvAttrIdMarker:
			a.pendingAttr = append(a.pendingAttr, Attribute{
				KeyStart: -1,
				ValStart: ev.Start,
				ValEnd:   ev.End,
			})
		case EvAttrClassMarker:
			a.pendingAttr = append(a.pendingAttr, Attribute{
				KeyStart: -2,
				ValStart: ev.Start,
				ValEnd:   ev.End,
			})
		case EvAttrKey:
			keyEv := ev
			if keyEv.Start < int32(len(a.doc.Source)) && a.doc.Source[keyEv.Start] == '=' {
				a.pendingAttr = append(a.pendingAttr, Attribute{
					KeyStart: -3,
					ValStart: keyEv.Start + 1,
					ValEnd:   keyEv.End,
				})
			} else {
				for i+1 < len(events) && events[i+1].Type != EvCloseAttributes {
					i++
					if events[i].Type == EvAttrValue {
						rawVal := a.doc.Source[events[i].Start : events[i].End+1]
						decodedVal := unescapeBytes(rawVal)
						valStart := int32(len(a.doc.Extra))
						a.doc.Extra = append(a.doc.Extra, decodedVal...)
						valEnd := int32(len(a.doc.Extra)) - 1
						a.pendingAttr = append(a.pendingAttr, Attribute{
							KeyStart: keyEv.Start,
							KeyEnd:   keyEv.End,
							ValStart: ^valStart,
							ValEnd:   valEnd,
						})
						break
					}
				}
			}
		}
		i++
	}
	return i
}

func (a *Assembler) getAttrVal(attr Attribute) []byte {
	if attr.ValStart < 0 {
		return a.doc.Extra[^attr.ValStart : attr.ValEnd+1]
	}
	return a.doc.Source[attr.ValStart : attr.ValEnd+1]
}

func (a *Assembler) applyPendingAttr(idx int32) {
	if len(a.pendingAttr) == 0 {
		return
	}

	node := &a.doc.Nodes[idx]
	var attrs []Attribute
	if node.Attr != -1 {
		for i := uint16(0); i < node.AttrCount; i++ {
			attrs = append(attrs, a.doc.Attributes[node.Attr+int32(i)])
		}
	}

	for _, newAttr := range a.pendingAttr {
		if newAttr.KeyStart == -1 {
			id := string(a.getAttrVal(newAttr))
			a.usedIDs[id] = true
		}

		foundIdx := -1
		for i := range attrs {
			if newAttr.KeyStart == -1 && attrs[i].KeyStart == -1 {
				foundIdx = i
				break
			} else if newAttr.KeyStart == -2 && attrs[i].KeyStart == -2 {
				foundIdx = i
				break
			} else if newAttr.KeyStart >= 0 && attrs[i].KeyStart >= 0 {
				oldKey := a.doc.Source[attrs[i].KeyStart : attrs[i].KeyEnd+1]
				newKey := a.doc.Source[newAttr.KeyStart : newAttr.KeyEnd+1]
				if bytes.Equal(oldKey, newKey) {
					foundIdx = i
					break
				}
			}
		}

		if newAttr.KeyStart == -2 && foundIdx != -1 {
			oldVal := a.getAttrVal(attrs[foundIdx])
			newVal := a.getAttrVal(newAttr)

			start := int32(len(a.doc.Extra))
			a.doc.Extra = append(a.doc.Extra, oldVal...)
			a.doc.Extra = append(a.doc.Extra, ' ')
			a.doc.Extra = append(a.doc.Extra, newVal...)
			end := int32(len(a.doc.Extra)) - 1

			attrs[foundIdx].ValStart = ^start
			attrs[foundIdx].ValEnd = end
		} else if foundIdx != -1 {
			attrs[foundIdx] = newAttr
		} else {
			attrs = append(attrs, newAttr)
		}
	}

	node.Attr = int32(len(a.doc.Attributes))
	node.AttrCount = uint16(len(attrs))
	a.doc.Attributes = append(a.doc.Attributes, attrs...)
	a.pendingAttr = a.pendingAttr[:0]
}

func (a *Assembler) handleDefinitionListItem(idx int32) {
	firstIdx := a.doc.Nodes[idx].Child
	if firstIdx != -1 && a.doc.Nodes[firstIdx].Type == NodePara {
		termIdx := firstIdx
		a.doc.Nodes[termIdx].Type = NodeTerm

		defIdx := a.addNode(NodeDefinition, a.doc.Nodes[termIdx].End, a.doc.Nodes[idx].End)
		a.doc.Nodes[defIdx].Child = a.doc.Nodes[termIdx].Next
		a.doc.Nodes[termIdx].Next = defIdx
	} else {
		termIdx := a.addNode(NodeTerm, a.doc.Nodes[idx].Start, a.doc.Nodes[idx].Start)
		defIdx := a.addNode(NodeDefinition, a.doc.Nodes[idx].Start, a.doc.Nodes[idx].End)
		a.doc.Nodes[defIdx].Child = firstIdx
		a.doc.Nodes[idx].Child = termIdx
		a.doc.Nodes[termIdx].Next = defIdx
	}
}

func (a *Assembler) isInline(t NodeType) bool {
	switch t {
	case NodeStr, NodeEmph, NodeStrong, NodeLink, NodeImage, NodeSpan, NodeVerbatim, NodeSubscript, NodeSuperscript, NodeInsert, NodeDelete, NodeMark, NodeDoubleQuoted, NodeSingleQuoted, NodeSmartPunctuation, NodeSoftBreak, NodeHardBreak, NodeInlineMath, NodeDisplayMath, NodeSymb, NodeUrl, NodeEmail, NodeFootnoteReference:
		return true
	}
	return false
}

func (a *Assembler) addNode(ntype NodeType, start, end int32) int32 {
	idx := int32(len(a.doc.Nodes))
	a.doc.Nodes = append(a.doc.Nodes, Node{
		Type:  ntype,
		Start: start,
		End:   end,
		Child: -1,
		Next:  -1,
		Attr:  -1,
	})
	return idx
}

func (a *Assembler) linkToParent(parentIdx, nodeIdx int32) {
	if parentIdx == -1 || nodeIdx == -1 || parentIdx == nodeIdx {
		return
	}
	parent := &a.doc.Nodes[parentIdx]
	if parent.Child == -1 {
		parent.Child = nodeIdx
	} else {
		curr := parent.Child
		if curr == nodeIdx {
			return
		}
		for a.doc.Nodes[curr].Next != -1 {
			if a.doc.Nodes[curr].Next == nodeIdx {
				return
			}
			curr = a.doc.Nodes[curr].Next
		}
		a.doc.Nodes[curr].Next = nodeIdx
	}
}

func (a *Assembler) openNode(ntype NodeType, start int32) int32 {
	var parentIdx int32 = -1
	if len(a.stack) > 0 {
		parentIdx = a.stack[len(a.stack)-1]
	}
	nodeIdx := a.addNode(ntype, start, -1)
	a.linkToParent(parentIdx, nodeIdx)
	a.stack = append(a.stack, nodeIdx)
	a.lastClosedNode = -1

	if ntype == NodeTable {
		a.currentTableAlignments = nil
	}
	return nodeIdx
}

func (a *Assembler) closeNode(end int32) int32 {
	if len(a.stack) <= 1 {
		return -1
	}
	nodeIdx := a.stack[len(a.stack)-1]
	node := &a.doc.Nodes[nodeIdx]
	if node.Type != NodeCodeBlock && node.Type != NodeRawBlock && node.Type != NodeDiv {
		node.End = end
	}
	a.stack = a.stack[:len(a.stack)-1]
	if a.isInline(a.doc.Nodes[nodeIdx].Type) {
		a.lastClosedNode = nodeIdx
	}
	return nodeIdx
}

func (a *Assembler) addLeafNode(ntype NodeType, start, end int32) {
	parentIdx := a.stack[len(a.stack)-1]

	if ntype == NodeStr && a.lastClosedNode != -1 {
		prevNode := &a.doc.Nodes[a.lastClosedNode]
		if prevNode.Type == NodeStr && prevNode.End+1 == start {
			prevNode.End = end
			return
		}
	}

	nodeIdx := a.addNode(ntype, start, end)
	a.linkToParent(parentIdx, nodeIdx)
	a.lastClosedNode = nodeIdx
}

func isOrderedMarker(marker byte) bool {
	switch marker {
	case '1', 'a', 'A', 'i', 'I':
		return true
	}
	return false
}

func (a *Assembler) setStyleFromMarker(idx int32, marker byte) {
	node := &a.doc.Nodes[idx]
	switch marker {
	case '1':
		node.Data |= DataListDecimal
	case 'a':
		node.Data |= DataListLowerAlpha
	case 'A':
		node.Data |= DataListUpperAlpha
	case 'i':
		node.Data |= DataListLowerRoman
	case 'I':
		node.Data |= DataListUpperRoman
	}
}
