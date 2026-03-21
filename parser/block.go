package parser

import (
	. "github.com/nein-ar/dejot/ast"
)

import (
	"bytes"
)

type ActiveContainer struct {
	Type  ContainerType
	Start int32
	End   int32
	Data  ContainerData
}
type BlockParser struct {
	source []byte
	pos    int32
	events []Event
	stack  []ActiveContainer

	inPara    bool
	paraLines [][2]int32

	pendingAttr []Event
	hasAttr     bool

	inAttrBlock    bool
	attrLines      [][2]int32
	lineIndent     int32
	afterBlankLine bool
}

func NewBlockParser(source []byte) *BlockParser {
	p := &BlockParser{
		source:         source,
		events:         make([]Event, 0, len(source)/8+32),
		stack:          make([]ActiveContainer, 0, 16),
		paraLines:      make([][2]int32, 0, 8),
		attrLines:      make([][2]int32, 0, 2),
		afterBlankLine: true,
	}
	p.stack = append(p.stack, ActiveContainer{Type: ContainerDoc})
	return p
}

func (p *BlockParser) addEvent(start, end int32, etype EventType) {
	p.events = append(p.events, Event{Start: start, End: end, Type: etype})
}

func (p *BlockParser) Parse() []Event {
	for p.pos < int32(len(p.source)) {
		lineStart := p.pos
		eol := bytes.IndexByte(p.source[p.pos:], '\n')
		var nextPos int32
		if eol == -1 {
			nextPos = int32(len(p.source))
		} else {
			nextPos = p.pos + int32(eol) + 1
		}

		p.handleLine(lineStart, nextPos)
		p.pos = nextPos
	}

	p.closePara()
	p.flushFailedAttr()
	p.closePara()
	p.discardDanglingAttr()
	for i := len(p.stack) - 1; i > 0; i-- {
		p.closeContainer(i)
	}
	return p.events
}

func (p *BlockParser) handleLine(start, end int32) {
	contentEnd := end
	if contentEnd > start && p.source[contentEnd-1] == '\n' {
		contentEnd--
		if contentEnd > start && p.source[contentEnd-1] == '\r' {
			contentEnd--
		}
	}

	p.lineIndent = 0
	for start+p.lineIndent < contentEnd && p.source[start+p.lineIndent] == ' ' {
		p.lineIndent++
	}

	matched := 1
	currentPos := start
	allMatched := true
	tipIdx := len(p.stack) - 1

	for i := 1; i < len(p.stack); i++ {
		c := &p.stack[i]
		if i == tipIdx && (c.Type == ContainerCodeBlock || c.Type == ContainerRawBlock) {
			if p.isClosingFence(c, currentPos, contentEnd) {
				p.closePara()
				p.closeContainer(i)
				return
			}
			p.closePara()
			p.handleLeafContent(c, currentPos, end)
			return
		}

		tipIsCode := p.stack[tipIdx].Type == ContainerCodeBlock ||
			p.stack[tipIdx].Type == ContainerRawBlock
		if !tipIsCode && c.Type == ContainerDiv {
			if p.isClosingFence(c, currentPos, contentEnd) {
				p.closePara()
				for j := len(p.stack) - 1; j >= i; j-- {
					p.closeContainer(j)
				}
				return
			}
		}

		if c.Type == ContainerCodeBlock || c.Type == ContainerRawBlock {
			break
		}

		newPos := p.checkContinue(c, currentPos, contentEnd)
		if newPos != -1 {
			matched++
			currentPos = newPos
			if isLeaf(c.Type) {
				break
			}
		} else {
			allMatched = false
			break
		}
	}
	isBlank := isBlankLine(p.source, currentPos, contentEnd)

	if isBlank {
		p.closePara()
		p.discardDanglingAttr()
		for i := len(p.stack) - 1; i >= matched; i-- {
			p.closeContainer(i)
		}
		if p.inAttrBlock {
			p.flushFailedAttr()
		}
		p.addEvent(start, end, EvBlankLine)
		p.markListBlankLines(matched)
		p.afterBlankLine = true
		return
	}

	defer func() { p.afterBlankLine = false }()

	if p.inAttrBlock {
		p.attrLines = append(p.attrLines, [2]int32{currentPos, contentEnd})
		p.tryParseBufferedAttr()
		return
	}

	if allMatched && !p.inAttrBlock && currentPos < contentEnd && p.source[currentPos] == '{' && (p.afterBlankLine || p.hasAttr) {
		p.inAttrBlock = true
		p.attrLines = append(p.attrLines, [2]int32{currentPos, contentEnd})
		p.tryParseBufferedAttr()
		return
	}

	newStarts := false
	if !allMatched || p.inPara || !isLeaf(p.stack[len(p.stack)-1].Type) {
		openPos := currentPos
		if openPos < start+p.lineIndent {
			openPos = start + p.lineIndent
		}
		for {
			for i := 0; i < 3 && openPos < contentEnd && p.source[openPos] == ' '; i++ {
				openPos++
			}

			ctype, nextOpenPos, data, ok := p.findOpen(start, openPos, contentEnd)
			if !ok {
				break
			}

			if p.inPara && !p.canInterrupt(ctype, openPos, contentEnd) {
				break
			}

			newStarts = true
			canHaveImmediateAttr := ctype == ContainerDiv || ctype == ContainerCodeBlock || ctype == ContainerRawBlock || ctype == ContainerHeading
			attrPos := nextOpenPos
			for attrPos < contentEnd && (p.source[attrPos] == ' ' || p.source[attrPos] == '\t') {
				attrPos++
			}
			if attrPos < contentEnd && canHaveImmediateAttr {
				if p.source[attrPos] == '{' {
					attrEvents, nextPos, ok := ParseAttributes(p.source, attrPos, contentEnd)
					if ok && isBlankLine(p.source, nextPos+1, contentEnd) {
						p.hasAttr = true
						p.pendingAttr = append(p.pendingAttr, attrEvents...)
						p.closePara()
						for i := len(p.stack) - 1; i >= matched; i-- {
							p.closeContainer(i)
						}
						p.openContainer(ctype, openPos, data)
						return
					}
				} else if ctype == ContainerDiv || ctype == ContainerCodeBlock || ctype == ContainerRawBlock {
					classStart := attrPos
					if ctype == ContainerDiv && p.source[classStart] == '.' {
						classStart++
					}
					classEnd := classStart
					for classEnd < contentEnd && !isSpace(p.source[classEnd]) {
						classEnd++
					}
					if classEnd > classStart {
						p.hasAttr = true
						if ctype == ContainerRawBlock {
							eqPos := attrPos - 1
							p.pendingAttr = append(p.pendingAttr, Event{Start: eqPos, End: classEnd - 1, Type: EvAttrKey})
						} else if ctype == ContainerCodeBlock && p.source[classStart] == '=' {
							ctype = ContainerRawBlock
							p.pendingAttr = append(p.pendingAttr, Event{Start: classStart, End: classEnd - 1, Type: EvAttrKey})
						} else {
							p.pendingAttr = append(p.pendingAttr, Event{Start: classStart, End: classEnd - 1, Type: EvAttrClassMarker})
						}
						nextAttrPos := classEnd
						for nextAttrPos < contentEnd && isSpace(p.source[nextAttrPos]) {
							nextAttrPos++
						}
						if nextAttrPos < contentEnd && p.source[nextAttrPos] == '{' {
							attrEvents, _, ok := ParseAttributes(p.source, nextAttrPos, contentEnd)
							if ok {
								p.pendingAttr = append(p.pendingAttr, attrEvents...)
							}
						}
						p.closePara()
						for i := len(p.stack) - 1; i >= matched; i-- {
							p.closeContainer(i)
						}
						p.openContainer(ctype, openPos, data)
						return
					}
				}
			}

			p.closePara()
			for i := len(p.stack) - 1; i >= matched; i-- {
				c := &p.stack[i]
				if c.Data.BlankLines && c.Type == ContainerListItem {
					c.Data.Tight = false
					if i > 0 && p.stack[i-1].Type == ContainerList {
						p.stack[i-1].Data.Tight = false
					}
				}
				p.closeContainer(i)
			}
			p.openContainer(ctype, openPos, data)

			currentPos = nextOpenPos
			openPos = nextOpenPos
			matched = len(p.stack)

			if currentPos < contentEnd && p.source[currentPos] == '{' && !p.inAttrBlock {
				p.inAttrBlock = true
				p.attrLines = append(p.attrLines, [2]int32{currentPos, contentEnd})
				p.tryParseBufferedAttr()
				return
			}

			if isLeaf(ctype) {
				if ctype == ContainerReferenceDef {
					currentPos = openPos
				}
				break
			}
		}
	}

	isLazy := !isBlank && !newStarts && matched < len(p.stack) && p.inPara

	if !isLazy {
		if matched < len(p.stack) {
			p.closePara()
			for i := len(p.stack) - 1; i >= matched; i-- {
				p.closeContainer(i)
			}
		}
	}

	tipIdx = len(p.stack) - 1
	tip := &p.stack[tipIdx]

	if tip.Type == ContainerTable {
		p.handleTableRow(currentPos, contentEnd)
		return
	}

	if isBlankLine(p.source, currentPos, contentEnd) {
		return
	}

	if hasInlineContent(tip.Type) || isBlock(tip.Type) || p.inPara {
		for currentPos < contentEnd && (p.source[currentPos] == ' ' || p.source[currentPos] == '\t') {
			currentPos++
		}
		p.accumulateInlineLine(currentPos, contentEnd)
	} else {
		p.handleLeafContent(tip, currentPos, end)
	}
}

func isBlock(t ContainerType) bool {
	switch t {
	case ContainerDoc, ContainerBlockQuote, ContainerListItem, ContainerList, ContainerDiv, ContainerFootnote:
		return true
	}
	return false
}

func hasInlineContent(t ContainerType) bool {
	switch t {
	case ContainerPara, ContainerHeading, ContainerCaption:
		return true
	}
	return false
}

func (p *BlockParser) accumulateInlineLine(start, end int32) {
	tipIdx := len(p.stack) - 1
	tip := &p.stack[tipIdx]
	if (tip.Type == ContainerPara || isBlock(tip.Type)) && !p.inPara {
		p.inPara = true
		if p.hasAttr {
			p.addEvent(start, start, EvOpenAttributes)
			p.events = append(p.events, p.pendingAttr...)
			p.addEvent(start, start, EvCloseAttributes)
			p.hasAttr = false
			p.pendingAttr = p.pendingAttr[:0]
		}
		p.addEvent(start, start, EvOpenPara)
	}
	p.paraLines = append(p.paraLines, [2]int32{start, end})
}

func (p *BlockParser) tryParseBufferedAttr() {
	if len(p.attrLines) == 0 {
		return
	}

	start := p.attrLines[0][0]
	end := p.attrLines[len(p.attrLines)-1][1]

	attrEvents, nextPos, ok := ParseAttributes(p.source, start, end)

	if ok {
		if isBlankLine(p.source, nextPos+1, end) {
			p.closePara()
			p.hasAttr = true
			p.pendingAttr = append(p.pendingAttr, attrEvents...)
			p.inAttrBlock = false
			p.attrLines = p.attrLines[:0]
			return
		} else {
			p.flushFailedAttr()
			return
		}
	} else {
		hasClosing := false
		for i := start; i < end; i++ {
			if p.source[i] == '}' {
				hasClosing = true
				break
			}
		}
		if hasClosing {
			p.flushFailedAttr()
		}
	}
}
func (p *BlockParser) flushFailedAttr() {
	p.inAttrBlock = false
	for _, line := range p.attrLines {
		start := line[0]
		for start < line[1] && (p.source[start] == ' ' || p.source[start] == '\t') {
			start++
		}
		p.accumulateInlineLine(start, line[1])
	}
	p.attrLines = p.attrLines[:0]
}

func (p *BlockParser) closePara() {
	if !p.inPara {
		if len(p.paraLines) > 0 {
			for i, line := range p.paraLines {
				start := line[0]
				end := line[1]
				if i == len(p.paraLines)-1 {
					for end > start && (p.source[end-1] == ' ' || p.source[end-1] == '\t') {
						end--
					}
				}
				isHardBreak := false
				if i < len(p.paraLines)-1 {
					tempEnd := end
					for tempEnd > start && (p.source[tempEnd-1] == ' ' || p.source[tempEnd-1] == '\t') {
						tempEnd--
					}
					if tempEnd > start && p.source[tempEnd-1] == '\\' {
						backslashCount := 0
						for j := tempEnd - 1; j >= start && p.source[j] == '\\'; j-- {
							backslashCount++
						}
						if backslashCount%2 != 0 {
							isHardBreak = true
							end = tempEnd - 1
							for end > start && (p.source[end-1] == ' ' || p.source[end-1] == '\t') {
								end--
							}
						}
					}
				}
				if start < end {
					p.addEvent(start, end-1, EvText)
				}
				if i < len(p.paraLines)-1 {
					if isHardBreak {
						p.addEvent(line[1], line[1], EvHardBreak)
					} else {
						p.addEvent(line[1], line[1], EvSoftBreak)
					}
				}
			}
			p.paraLines = p.paraLines[:0]
		}
		return
	}

	for i, line := range p.paraLines {
		start := line[0]
		end := line[1]
		if i == len(p.paraLines)-1 {
			for end > start && (p.source[end-1] == ' ' || p.source[end-1] == '\t') {
				end--
			}
		}
		isHardBreak := false
		if i < len(p.paraLines)-1 {
			tempEnd := end
			for tempEnd > start && (p.source[tempEnd-1] == ' ' || p.source[tempEnd-1] == '\t') {
				tempEnd--
			}
			if tempEnd > start && p.source[tempEnd-1] == '\\' {
				backslashCount := 0
				for j := tempEnd - 1; j >= start && p.source[j] == '\\'; j-- {
					backslashCount++
				}
				if backslashCount%2 != 0 {
					isHardBreak = true
					end = tempEnd - 1
					for end > start && (p.source[end-1] == ' ' || p.source[end-1] == '\t') {
						end--
					}
				}
			}
		}
		if start < end {
			p.addEvent(start, end-1, EvText)
		}
		if i < len(p.paraLines)-1 {
			if isHardBreak {
				p.addEvent(line[1], line[1], EvHardBreak)
			} else {
				p.addEvent(line[1], line[1], EvSoftBreak)
			}
		}
	}

	if len(p.paraLines) > 0 {
		lastLine := p.paraLines[len(p.paraLines)-1]
		p.addEvent(lastLine[1], lastLine[1], EvClosePara)
	}
	p.inPara = false
	p.paraLines = p.paraLines[:0]
}

func (p *BlockParser) discardDanglingAttr() {
	p.hasAttr = false
	p.pendingAttr = p.pendingAttr[:0]
}

func isLeaf(t ContainerType) bool {
	switch t {
	case ContainerPara, ContainerHeading, ContainerCodeBlock, ContainerRawBlock, ContainerThematicBreak, ContainerReferenceDef, ContainerCaption, ContainerTable:
		return true
	}
	return false
}

func isBlankLine(source []byte, start, end int32) bool {
	for i := start; i < end; i++ {
		if source[i] != ' ' && source[i] != '\t' {
			return false
		}
	}
	return true
}

func (p *BlockParser) markListBlankLines(matched int) {
}

func (p *BlockParser) checkContinue(c *ActiveContainer, pos, end int32) int32 {
	switch c.Type {
	case ContainerBlockQuote:
		if pos < end && p.source[pos] == '>' {
			pos++
			if pos == end {
				return pos
			}
			if p.source[pos] == ' ' {
				return pos + 1
			}
			return -1
		}
	case ContainerHeading:
		if isBlankLine(p.source, pos, end) {
			return -1
		}
		hLevel := int32(0)
		for pos+hLevel < end && p.source[pos+hLevel] == '#' {
			hLevel++
		}
		if hLevel > 0 && hLevel <= 6 && (pos+hLevel == end || p.source[pos+hLevel] == ' ' || p.source[pos+hLevel] == '\t') {
			if hLevel == int32(c.Data.Level) {
				newPos := pos + hLevel
				if newPos < end && (p.source[newPos] == ' ' || p.source[newPos] == '\t') {
					newPos++
				}
				return newPos
			}
			return -1
		}
		return pos
	case ContainerCodeBlock, ContainerRawBlock:
		if p.isClosingFence(c, pos, end) {
			return -1
		}
		return pos
	case ContainerDiv:
		return pos
	case ContainerList:
		return pos
	case ContainerListItem:
		if isBlankLine(p.source, pos, end) {
			return pos
		}
		if p.lineIndent > c.Data.Indent {
			return pos
		}
		if p.inPara && !p.afterBlankLine {
			markerPos := pos + p.lineIndent
			_, _, ok := parseListMarker(p.source, markerPos, end)
			if !ok {
				return pos
			}
		}
		return -1
	case ContainerTable:
		if pos < end && p.source[pos] == '|' {
			return pos
		}
	case ContainerFootnote:
		if isBlankLine(p.source, pos, end) {
			return pos
		}
		if p.lineIndent > c.Data.Indent {
			return pos
		}
		return -1
	case ContainerReferenceDef:
		if isBlankLine(p.source, pos, end) {
			return pos
		}
		if p.lineIndent > c.Data.Indent {
			return pos
		}
		return -1
	case ContainerCaption:
		if isBlankLine(p.source, pos, end) {
			return -1
		}
		return pos
	}
	return -1
}

func (p *BlockParser) isClosingFence(c *ActiveContainer, pos, end int32) bool {
	i := 0
	for i < 3 && pos < end && p.source[pos] == ' ' {
		pos++
		i++
	}

	char := c.Data.FenceChar
	count := int32(0)
	for pos < end && p.source[pos] == char {
		count++
		pos++
	}

	if count < c.Data.FenceLen {
		return false
	}

	for pos < end && (p.source[pos] == ' ' || p.source[pos] == '\t') {
		pos++
	}

	return pos == end || p.source[pos] == '\r' || p.source[pos] == '\n'
}

func (p *BlockParser) findOpen(start, pos, end int32) (ContainerType, int32, ContainerData, bool) {
	if pos >= end {
		return ContainerNone, pos, ContainerData{}, false
	}
	b := p.source[pos]

	if isThematicBreak(p.source, pos, end) {
		return ContainerThematicBreak, end, ContainerData{}, true
	}

	if b == '`' || b == '~' || b == ':' {
		count := int32(0)
		for i := pos; i < end && p.source[i] == b; i++ {
			count++
		}
		if count >= 3 {
			if b == '`' || b == '~' {
				rest := p.source[pos+count : end]
				hasInvalid := false
				for j := 0; j < len(rest); j++ {
					if rest[j] == '{' {
						break
					}
					if rest[j] == b {
						c := int32(0)
						for j < len(rest) && rest[j] == b {
							c++
							j++
						}
						if c >= count {
							hasInvalid = true
							break
						}
						j--
						continue
					}
					if rest[j] != ' ' && rest[j] != '	' {
						k := j
						for k < len(rest) && rest[k] != ' ' && rest[k] != '	' && rest[k] != '{' {
							if rest[k] == b {
								c := int32(0)
								for k < len(rest) && rest[k] == b {
									c++
									k++
								}
								if c >= count {
									hasInvalid = true
									break
								}
								continue
							}
							k++
						}
						if hasInvalid {
							break
						}
						for k < len(rest) && (rest[k] == ' ' || rest[k] == '	') {
							k++
						}
						if k < len(rest) && rest[k] != '{' {
							hasInvalid = true
							break
						}
						j = k - 1
					}
				}
				if hasInvalid {
					return ContainerNone, pos, ContainerData{}, false
				}
			}
			if b == ':' {
				return ContainerDiv, pos + count, ContainerData{FenceChar: b, FenceLen: count, Indent: pos - start}, true
			}
			classPos := pos + count
			for classPos < end && p.source[classPos] == ' ' {
				classPos++
			}
			if classPos < end && p.source[classPos] == '=' {
				return ContainerRawBlock, classPos + 1, ContainerData{FenceChar: b, FenceLen: count, Indent: pos - start}, true
			}
			return ContainerCodeBlock, pos + count, ContainerData{FenceChar: b, FenceLen: count, Indent: pos - start}, true
		}
	}

	if b == '#' {
		level := int32(0)
		for i := pos; i < end && p.source[i] == '#'; i++ {
			level++
		}
		if level > 0 && level <= 6 && (pos+level == end || p.source[pos+level] == ' ') {
			newPos := pos + level
			if newPos < end && p.source[newPos] == ' ' {
				newPos++
			}
			return ContainerHeading, newPos, ContainerData{Level: int16(level)}, true
		}
	}

	if b == '>' {
		newPos := pos + 1
		if newPos == end {
			return ContainerBlockQuote, newPos, ContainerData{}, true
		}
		if p.source[newPos] == ' ' {
			return ContainerBlockQuote, newPos + 1, ContainerData{}, true
		}
		return ContainerNone, pos, ContainerData{}, false
	}

	if b == '|' {
		pipes := 0
		i := pos
		for i < end {
			if p.source[i] == '|' {
				pipes++
				i++
			} else if p.source[i] == '`' {
				count := int32(0)
				for i < end && p.source[i] == '`' {
					count++
					i++
				}
				for i < end {
					if p.source[i] == '`' {
						c := int32(0)
						for i < end && p.source[i] == '`' {
							c++
							i++
						}
						if c == count {
							break
						}
					} else {
						i++
					}
				}
			} else {
				i++
			}
		}
		if pipes >= 2 {
			return ContainerTable, pos, ContainerData{}, true
		}
	}

	if b == '^' {
		if pos+1 < end && p.source[pos+1] == ' ' {
			return ContainerCaption, pos + 2, ContainerData{}, true
		}
	}

	if b == '[' {
		if pos+1 < end && p.source[pos+1] == '^' {
			for i := pos + 2; i < end; i++ {
				if p.source[i] == ']' && i+1 < end && p.source[i+1] == ':' {
					newPos := i + 2
					for newPos < end && (p.source[newPos] == ' ' || p.source[newPos] == '\t') {
						newPos++
					}
					return ContainerFootnote, newPos, ContainerData{Indent: p.lineIndent, MarkerWidth: newPos - pos}, true
				}
			}
		} else {
			for i := pos + 1; i < end; i++ {
				if p.source[i] == ']' && i+1 < end && p.source[i+1] == ':' {
					newPos := i + 2
					for newPos < end && (p.source[newPos] == ' ' || p.source[newPos] == '\t') {
						newPos++
					}
					return ContainerReferenceDef, newPos, ContainerData{Indent: p.lineIndent}, true
				}
			}
		}
	}

	newPos, data, ok := parseListMarker(p.source, pos, end)
	if ok {
		data.Indent = pos - start
		if !p.canListMarkerInterrupt(p.lineIndent, data.Marker, p.afterBlankLine) {
			return ContainerNone, pos, ContainerData{}, false
		}
		return ContainerListItem, newPos, data, true
	}

	return ContainerNone, pos, ContainerData{}, false
}

func (p *BlockParser) openContainer(ctype ContainerType, start int32, data ContainerData) {
	if p.hasAttr {
		p.addEvent(start, start, EvOpenAttributes)
		p.events = append(p.events, p.pendingAttr...)
		p.addEvent(start, start, EvCloseAttributes)
		p.hasAttr = false
		p.pendingAttr = p.pendingAttr[:0]
	}

	switch ctype {
	case ContainerHeading:
		p.addEvent(start, start+int32(data.Level)-1, EvOpenHeading)
	case ContainerBlockQuote:
		p.addEvent(start, start, EvOpenBlockQuote)
	case ContainerListItem:
		tipIdx := len(p.stack) - 1
		tip := &p.stack[tipIdx]
		if tip.Type == ContainerListItem {
			pIdx := len(p.stack) - 2
			parent := &p.stack[pIdx]
			if parent.Type == ContainerList {
				itemIndent := tip.Data.Indent
				if data.Indent > itemIndent {
					p.openList(start, data)
				} else if data.Indent == parent.Data.Indent {
					p.closeContainer(tipIdx)
					if parent.Data.Marker == data.Marker && parent.Data.MarkerEnd == data.MarkerEnd && parent.Data.Ordered == data.Ordered {
						if data.Ordered {
							parent.Data.NextNumber++
						}
					} else {
						p.closeContainer(pIdx)
						p.openList(start, data)
					}
				} else {
					p.closeContainer(tipIdx)
					p.closeContainer(pIdx)
					p.openList(start, data)
				}
			} else {
				p.openList(start, data)
			}
		} else if tip.Type == ContainerList {
			if tip.Data.Marker == data.Marker && tip.Data.MarkerEnd == data.MarkerEnd && tip.Data.Ordered == data.Ordered {
				if !data.MarkerAmbiguous {
					tip.Data.MarkerAmbiguous = false
				}
			} else if data.Indent == tip.Data.Indent {
				if p.tryNarrowList(tip, data) {
				} else {
					p.closeContainer(tipIdx)
					p.openList(start, data)
				}
			} else {
				p.openList(start, data)
			}
		} else {
			p.openList(start, data)
		}

		taskStatus := int32(0)
		if data.IsTask {
			if data.Checked {
				taskStatus = 2
			} else {
				taskStatus = 1
			}
		}
		p.addEvent(start, taskStatus, EvOpenListItem)
	case ContainerCodeBlock:
		p.addEvent(start, start+data.FenceLen-1, EvOpenCodeBlock)
		p.addEvent(start, start, EvNone)
	case ContainerRawBlock:
		p.addEvent(start, start+data.FenceLen-1, EvOpenRawBlock)
		p.addEvent(start, start, EvNone)
	case ContainerDiv:
		p.addEvent(start, start+data.FenceLen-1, EvOpenDiv)
		p.addEvent(start, start, EvNone)
	case ContainerThematicBreak:
		p.addEvent(start, start, EvThematicBreak)
	case ContainerTable:
		p.addEvent(start, start, EvOpenTable)
	case ContainerCaption:
		p.addEvent(start, start, EvOpenCaption)
	case ContainerFootnote:
		p.addEvent(start, start, EvOpenFootnote)
		p.addEvent(start, start+data.MarkerWidth-1, EvStr)
	case ContainerReferenceDef:
		p.addEvent(start, start, EvOpenReferenceDefinition)
	}
	p.stack = append(p.stack, ActiveContainer{Type: ctype, Start: start, Data: data})
}

func (p *BlockParser) closeContainer(idx int) {
	c := p.stack[idx]
	switch c.Type {
	case ContainerHeading:
		p.addEvent(c.Start, c.Start, EvCloseHeading)
	case ContainerBlockQuote:
		p.addEvent(c.Start, c.Start, EvCloseBlockQuote)
	case ContainerListItem:
		p.addEvent(c.Start, c.Start, EvCloseListItem)
	case ContainerList:
		tight := int32(0)
		if c.Data.Tight {
			tight = 1
		}
		p.addEvent(c.Start, tight, EvCloseList)
	case ContainerCodeBlock:
		p.addEvent(c.Start, c.Start, EvCloseCodeBlock)
	case ContainerRawBlock:
		p.addEvent(c.Start, c.Start, EvCloseRawBlock)
	case ContainerDiv:
		p.addEvent(c.Start, c.Start, EvCloseDiv)
	case ContainerTable:
		p.addEvent(c.Start, c.Start, EvCloseTable)
	case ContainerCaption:
		p.addEvent(c.Start, c.Start, EvCloseCaption)
	case ContainerFootnote:
		p.addEvent(c.Start, c.Start, EvCloseFootnote)
	case ContainerReferenceDef:
		p.addEvent(c.Start, c.Start, EvCloseReferenceDefinition)
	}
	p.stack = p.stack[:idx]
}

func (p *BlockParser) handleLeafContent(tip *ActiveContainer, start, end int32) {
	contentEnd := end
	if contentEnd > start && p.source[contentEnd-1] == '\n' {
		contentEnd--
		if contentEnd > start && p.source[contentEnd-1] == '\r' {
			contentEnd--
		}
	}

	if p.hasAttr {
		p.addEvent(start, start, EvOpenAttributes)
		p.events = append(p.events, p.pendingAttr...)
		p.addEvent(start, start, EvCloseAttributes)
		p.hasAttr = false
		p.pendingAttr = p.pendingAttr[:0]
	}
	switch tip.Type {
	case ContainerCodeBlock, ContainerRawBlock, ContainerDiv:
		if p.isClosingFence(tip, start, end) {
			p.closeContainer(len(p.stack) - 1)
			return
		}

		indentToStrip := tip.Data.Indent
		current := start
		for indentToStrip > 0 && current < end && p.source[current] == ' ' {
			current++
			indentToStrip--
		}
		if end < int32(len(p.source)) && p.source[end] == '\n' {
			end++
		} else if end < int32(len(p.source)) && p.source[end] == '\r' {
			end++
			if end < int32(len(p.source)) && p.source[end] == '\n' {
				end++
			}
		}
		p.addEvent(current, end-1, EvStr)
	case ContainerTable:
		p.handleTableRow(start, end)
	case ContainerHeading, ContainerCaption:
		p.accumulateInlineLine(start, end)
	case ContainerReferenceDef:
		actualEnd := contentEnd - 1
		if actualEnd >= int32(len(p.source)) {
			actualEnd = int32(len(p.source)) - 1
		}
		if tip.Start <= actualEnd {
			p.addEvent(tip.Start, actualEnd, EvStr)
		}
	case ContainerFootnote:
		actualEnd := contentEnd - 1
		if actualEnd >= int32(len(p.source)) {
			actualEnd = int32(len(p.source)) - 1
		}
		if actualEnd >= start {
			p.addEvent(start, actualEnd, EvStr)
		}
	}
}

func (p *BlockParser) handleTableRow(start, end int32) {
	isSep := false
	hasDash := false
	for i := start; i < end; i++ {
		b := p.source[i]
		if b == '-' || b == ':' {
			hasDash = true
		} else if b != '|' && b != ' ' && b != '\t' {
			hasDash = false
			break
		}
	}
	isSep = hasDash

	if isSep {
		p.addEvent(start, end, EvStr)
		return
	}

	p.addEvent(start, start, EvOpenRow)
	pos := start
	for pos < end {
		if p.source[pos] == '|' {
			pos++
			if pos >= end {
				break
			}
			cellStart := pos
			for pos < end {
				if p.source[pos] == '\\' && pos+1 < end {
					pos += 2
				} else if p.source[pos] == '`' {
					count := int32(0)
					for pos < end && p.source[pos] == '`' {
						count++
						pos++
					}
					for pos < end {
						if p.source[pos] == '`' {
							c := int32(0)
							for pos < end && p.source[pos] == '`' {
								c++
								pos++
							}
							if c == count {
								break
							}
						} else {
							pos++
						}
					}
				} else if p.source[pos] == '|' {
					break
				} else {
					pos++
				}
			}
			cellEnd := pos - 1
			for cellStart <= cellEnd && isSpace(p.source[cellStart]) {
				cellStart++
			}
			for cellEnd >= cellStart && isSpace(p.source[cellEnd]) {
				cellEnd--
			}
			p.addEvent(cellStart, cellStart, EvOpenCell)
			if cellStart <= cellEnd {
				p.addEvent(cellStart, cellEnd, EvText)
			}
			p.addEvent(pos, pos, EvCloseCell)
		} else {
			pos++
		}
	}
	p.addEvent(end, end, EvCloseRow)
}

func (p *BlockParser) canListMarkerInterrupt(markerIndent int32, markerChar byte, afterBlankLine bool) bool {
	isDefinition := markerChar == ':'

	for i := len(p.stack) - 1; i >= 0; i-- {
		c := p.stack[i]
		if c.Type == ContainerList || c.Type == ContainerListItem {
			parentMarker := c.Data.Marker
			parentIsDefinition := parentMarker == ':'
			parentIndent := c.Data.Indent

			if isDefinition == parentIsDefinition {
				if !afterBlankLine && markerIndent > parentIndent && markerIndent < parentIndent+4 {
					return false
				}
			}
			return true
		}
	}
	return true
}

func (p *BlockParser) canInterrupt(ctype ContainerType, pos, end int32) bool {
	switch ctype {
	case ContainerFootnote:
		return true
	case ContainerListItem:
		_, data, ok := parseListMarker(p.source, pos, end)
		if !ok {
			return false
		}
		if data.Ordered && data.NextNumber != 1 {
			alreadyInOrderedList := false
			canNarrowExistingList := false
			canContinueAsRoman := false
			hasDifferentMarkerList := false
			for i := len(p.stack) - 1; i >= 0; i-- {
				c := p.stack[i]
				if c.Type == ContainerList && c.Data.Ordered &&
					c.Data.Marker == data.Marker && c.Data.MarkerEnd == data.MarkerEnd &&
					c.Data.Indent == p.lineIndent {
					alreadyInOrderedList = true
					break
				}
				if c.Type == ContainerList && c.Data.Ordered &&
					c.Data.Indent == p.lineIndent {
					if c.Data.Marker != data.Marker || c.Data.MarkerEnd != data.MarkerEnd {
						hasDifferentMarkerList = true
					}
					if c.Data.Marker == 'i' && data.Marker == 'a' && c.Data.MarkerAmbiguous && !isRomanLowerStart(data.MarkerFirstChar) {
						canNarrowExistingList = true
						break
					}
					if c.Data.Marker == 'I' && data.Marker == 'A' && c.Data.MarkerAmbiguous && !isRomanUpperStart(data.MarkerFirstChar) {
						canNarrowExistingList = true
						break
					}
					if c.Data.Marker == 'i' && data.Marker == 'a' && data.MarkerAmbiguous && isRomanLowerStart(data.MarkerFirstChar) {
						if parseRoman([]byte{data.MarkerFirstChar}) == c.Data.NextNumber {
							canContinueAsRoman = true
							break
						}
					}
					if c.Data.Marker == 'I' && data.Marker == 'A' && data.MarkerAmbiguous && isRomanUpperStart(data.MarkerFirstChar) {
						if parseRoman([]byte{data.MarkerFirstChar}) == c.Data.NextNumber {
							canContinueAsRoman = true
							break
						}
					}
				}
			}
			if !alreadyInOrderedList && !canNarrowExistingList && !canContinueAsRoman && !hasDifferentMarkerList {
				return false
			}
		}
		return p.canListMarkerInterrupt(p.lineIndent, data.Marker, p.afterBlankLine)
	}
	return false
}
func (p *BlockParser) tryNarrowList(list *ActiveContainer, newItem ContainerData) bool {
	lower := list.Data.Marker == 'i'
	upper := list.Data.Marker == 'I'
	if !lower && !upper {
		return false
	}

	if lower {
		if list.Data.MarkerAmbiguous && newItem.Marker == 'a' && !isRomanLowerStart(newItem.MarkerFirstChar) {
			startNum := int(list.Data.MarkerFirstChar - 'a' + 1)
			p.events[list.Data.OpenListEventIdx].End = int32('a')
			p.events[list.Data.OpenListEventIdx+1].End = int32(startNum)
			list.Data.Marker = 'a'
			list.Data.MarkerAmbiguous = false
			return true
		}
		if !list.Data.MarkerAmbiguous && newItem.Marker == 'a' && !isRomanLowerStart(newItem.MarkerFirstChar) {
			return false
		}
		if !list.Data.MarkerAmbiguous && newItem.Marker == 'a' && isRomanLowerStart(newItem.MarkerFirstChar) {
			if parseRoman([]byte{newItem.MarkerFirstChar}) == list.Data.NextNumber {
				list.Data.NextNumber++
				return true
			}
		}
		if !list.Data.MarkerAmbiguous && newItem.Marker == 'i' && newItem.MarkerAmbiguous {
			if newItem.NextNumber == list.Data.NextNumber {
				list.Data.NextNumber++
				return true
			}
		}
		if !list.Data.MarkerAmbiguous && newItem.Marker == 'a' && newItem.MarkerAmbiguous && isRomanLowerStart(newItem.MarkerFirstChar) {
			if parseRoman([]byte{newItem.MarkerFirstChar}) == list.Data.NextNumber {
				list.Data.NextNumber++
				return true
			}
		}
	}

	if upper {
		if list.Data.MarkerAmbiguous && newItem.Marker == 'A' && !isRomanUpperStart(newItem.MarkerFirstChar) {
			startNum := int(list.Data.MarkerFirstChar - 'A' + 1)
			p.events[list.Data.OpenListEventIdx].End = int32('A')
			p.events[list.Data.OpenListEventIdx+1].End = int32(startNum)
			list.Data.Marker = 'A'
			list.Data.MarkerAmbiguous = false
			return true
		}
		if !list.Data.MarkerAmbiguous && newItem.Marker == 'A' && isRomanUpperStart(newItem.MarkerFirstChar) {
			if parseRoman([]byte{newItem.MarkerFirstChar}) == list.Data.NextNumber {
				list.Data.NextNumber++
				return true
			}
		}
		if !list.Data.MarkerAmbiguous && newItem.Marker == 'I' && newItem.MarkerAmbiguous {
			if newItem.NextNumber == list.Data.NextNumber {
				list.Data.NextNumber++
				return true
			}
		}
		if !list.Data.MarkerAmbiguous && newItem.Marker == 'A' && newItem.MarkerAmbiguous && isRomanUpperStart(newItem.MarkerFirstChar) {
			if parseRoman([]byte{newItem.MarkerFirstChar}) == list.Data.NextNumber {
				list.Data.NextNumber++
				return true
			}
		}
	}
	return false
}

func (p *BlockParser) openList(start int32, data ContainerData) {
	data.OpenListEventIdx = int32(len(p.events))
	p.addEvent(start, int32(data.Marker), EvOpenList)
	p.addEvent(start, int32(data.NextNumber), EvNone)
	p.stack = append(p.stack, ActiveContainer{Type: ContainerList, Start: start, Data: data})
	p.stack[len(p.stack)-1].Data.NextNumber++
}
