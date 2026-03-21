package parser

import (
	. "github.com/nein-ar/dejot/ast"
)

type Opener struct {
	MatchIndex int32
	Start      int32
	Type       EventType
	Char       byte
	Explicit   bool
}

type InlineParser struct {
	source []byte
	events []Event
	stack  []Opener
}

func NewInlineParser(source []byte) *InlineParser {
	return &InlineParser{
		source: source,
		events: make([]Event, 0, 256),
		stack:  make([]Opener, 0, 16),
	}
}

func (p *InlineParser) Reset() {
	p.stack = p.stack[:0]
}

func (p *InlineParser) Parse(blockEvents []Event) []Event {
	var textEvents []Event
	for i := 0; i < len(blockEvents); i++ {
		ev := blockEvents[i]
		if ev.Type == EvText || ev.Type == EvSoftBreak || ev.Type == EvHardBreak {
			textEvents = append(textEvents, ev)
		} else {
			if len(textEvents) > 0 {
				p.parseInlineBlock(textEvents)
				textEvents = textEvents[:0]
			}
			if ev.Type == EvClosePara || ev.Type == EvCloseHeading || ev.Type == EvCloseCaption || ev.Type == EvCloseListItem || ev.Type == EvCloseCell {
				p.Trim()
			}
			p.events = append(p.events, ev)
			p.Reset()
		}
	}
	if len(textEvents) > 0 {
		p.parseInlineBlock(textEvents)
	}

	return p.events
}

func (p *InlineParser) Trim() {
	if len(p.events) == 0 {
		return
	}
	i := len(p.events) - 1
	if p.events[i].Type == EvSoftBreak || p.events[i].Type == EvHardBreak {
		p.events = p.events[:i]
		i--
		if i >= 0 && p.events[i].Type == EvStr {
			end := p.events[i].End
			start := p.events[i].Start
			for end >= start && (p.source[end] == ' ' || p.source[end] == '\t') {
				end--
			}
			if end < start {
				p.events = p.events[:i]
			} else {
				p.events[i].End = end
			}
		}
	}
}

func (p *InlineParser) findNextSpecialAhead(pos int32, end int32) int32 {
	for i := pos; i <= end; i++ {
		if isSpecial(p.source[i]) || p.source[i] == '{' {
			return i
		}
	}
	return end + 1
}

func (p *InlineParser) parseInlineBlock(textEvents []Event) {
	if len(textEvents) == 0 {
		return
	}

	blockEnd := textEvents[len(textEvents)-1].End

	evIdx := 0
	for evIdx < len(textEvents) {
		ev := textEvents[evIdx]
		if ev.Type == EvSoftBreak {
			p.addEvent(ev.Start, ev.End, EvSoftBreak)
			evIdx++
			continue
		}
		if ev.Type == EvHardBreak {
			p.addEvent(ev.Start, ev.End, EvHardBreak)
			evIdx++
			continue
		}

		start := ev.Start
		end := ev.End
		pos := start

		for pos <= end {
			if pos >= int32(len(p.source)) {
				break
			}

			b := p.source[pos]

			if !isSpecial(b) && b != '{' {
				next := p.findNextSpecialAhead(pos+1, end)
				p.addEvent(pos, next-1, EvStr)
				pos = next
				continue
			}

			if b == '{' {
				if pos+1 <= end {
					next := p.source[pos+1]
					if next == '_' || next == '*' || next == '~' || next == '^' || next == '-' || next == '+' || next == '=' || next == '\'' || next == '"' {
						p.addEvent(pos, pos, EvOpenInlineAttributes)
						newPos := p.handleEmphasis(next, pos+1, end)
						pos = newPos
						continue
					}
				}

				attrEvents, nextPos, ok := ParseAttributes(p.source, pos, blockEnd+1)
				if ok {
					p.handleAttributeAttachment(pos, nextPos, attrEvents)
					pos = nextPos + 1
					for evIdx < len(textEvents) && pos > textEvents[evIdx].End {
						evIdx++
					}
					if evIdx < len(textEvents) && pos >= textEvents[evIdx].Start {
						end = textEvents[evIdx].End
						start = pos
					}
					continue
				} else {
					p.addEvent(pos, pos, EvStr)
					pos++
					continue
				}
			}

			switch b {
			case '\\':
				if pos+1 <= end {
					next := p.source[pos+1]
					if next == ' ' {
						p.addEvent(pos, pos+1, EvNonBreakingSpace)
						pos += 2
					} else if isPunctuation(next) {
						p.addEvent(pos, pos+1, EvEscape)
						pos += 2
					} else {
						p.addEvent(pos, pos, EvStr)
						pos++
					}
				} else {
					p.addEvent(pos, pos, EvStr)
					pos++
				}
			case '*':
				pos = p.handleEmphasis(b, pos, end)
			case '_':
				pos = p.handleEmphasis(b, pos, end)
			case '^':
				pos = p.handleEmphasis(b, pos, end)
			case '~':
				pos = p.handleEmphasis(b, pos, end)
			case '"':
				p.handleDoubleQuote(pos, end)
				pos++
			case '\'':
				pos = p.handleSingleQuote(pos, end)
			case '[':
				if pos+1 <= blockEnd && p.source[pos+1] == '^' {
					closeIdx := p.findClosingBracketAhead(pos, textEvents, evIdx)
					if closeIdx != -1 {
						p.addEvent(pos, closeIdx, EvFootnoteReference)
						pos = closeIdx + 1
						for evIdx < len(textEvents) && pos > textEvents[evIdx].End {
							evIdx++
						}
						if evIdx < len(textEvents) && pos >= textEvents[evIdx].Start {
							end = textEvents[evIdx].End
							start = pos
						}
						continue
					}
				}
				p.addOpener(b, pos, EvOpenLinkText, false)
				pos++
			case '!':
				if pos+1 <= end && p.source[pos+1] == '[' {
					p.addOpener('[', pos+1, EvOpenImageText, false)
					pos += 2
				} else {
					p.addEvent(pos, pos, EvStr)
					pos++
				}
			case ']':
				pos = p.handleCloseBracketAhead(pos, textEvents, evIdx, blockEnd)
				for evIdx < len(textEvents) && pos > textEvents[evIdx].End {
					evIdx++
				}
				if evIdx < len(textEvents) && pos >= textEvents[evIdx].Start {
					end = textEvents[evIdx].End
					start = pos
				}
			case '(':
				if len(p.events) > 0 && p.events[len(p.events)-1].Type == EvCloseLinkText {
					destEnd := p.findClosingParenAhead(pos+1, textEvents, evIdx)
					if destEnd != -1 {
						p.addEvent(pos+1, destEnd-1, EvOpenDestination)
						p.addEvent(pos+1, destEnd-1, EvCloseDestination)
						pos = destEnd + 1
						for evIdx < len(textEvents) && pos > textEvents[evIdx].End {
							evIdx++
						}
						if evIdx < len(textEvents) && pos >= textEvents[evIdx].Start {
							end = textEvents[evIdx].End
							start = pos
						}
						continue
					}
				}
				p.addEvent(pos, pos, EvStr)
				pos++
			case '`':
				pos = p.handleVerbatimAhead(pos, textEvents, evIdx)
				for evIdx < len(textEvents) && pos > textEvents[evIdx].End {
					evIdx++
				}
				if evIdx < len(textEvents) && pos >= textEvents[evIdx].Start {
					end = textEvents[evIdx].End
					start = pos
				}

				if pos < blockEnd && p.source[pos] == '{' {
					attrEvents, nextPos, ok := ParseAttributes(p.source, pos, blockEnd+1)
					if ok {
						isRaw := false
						if len(attrEvents) == 1 {
							ae := attrEvents[0]
							if ae.Type == EvAttrKey && p.source[ae.Start] == '=' {
								isRaw = true
							}
						}

						if isRaw {
							p.events[len(p.events)-2].Type = EvRawInline
							p.events[len(p.events)-1].Type = EvRawInline
							p.handleAttributeAttachment(pos, nextPos, attrEvents)
							pos = nextPos + 1
						}

						for evIdx < len(textEvents) && pos > textEvents[evIdx].End {
							evIdx++
						}
						if evIdx < len(textEvents) && pos >= textEvents[evIdx].Start {
							end = textEvents[evIdx].End
							start = pos
						}
					}
				}
			case '.':
				if pos+2 <= end && p.source[pos+1] == '.' && p.source[pos+2] == '.' {
					p.addEvent(pos, pos+2, EvSmartPunctuation)
					pos += 3
				} else {
					p.addEvent(pos, pos, EvStr)
					pos++
				}
			case '-':
				if (pos > 0 && p.source[pos-1] == '{') || (pos+1 <= end && p.source[pos+1] == '}') {
					pos = p.handleEmphasis(b, pos, end)
					continue
				}

				count := int32(0)
				for pos+count <= end && p.source[pos+count] == '-' {
					count++
				}

				if pos+count <= end && p.source[pos+count] == '}' {
					if count > 1 {
						count--
					} else {
						pos = p.handleEmphasis(b, pos, end)
						continue
					}
				}

				if count == 1 {
					p.addEvent(pos, pos, EvStr)
					pos++
				} else {
					emd := int32(0)
					end2 := int32(0)
					if count%3 == 0 {
						emd = count / 3
						end2 = 0
					} else if count%2 == 0 {
						emd = 0
						end2 = count / 2
					} else if count%3 == 2 {
						emd = count / 3
						end2 = 1
					} else {
						emd = (count - 4) / 3
						end2 = 2
					}
					p2 := pos
					for i := int32(0); i < emd; i++ {
						p.addEvent(p2, p2+2, EvSmartPunctuation)
						p2 += 3
					}
					for i := int32(0); i < end2; i++ {
						p.addEvent(p2, p2+1, EvSmartPunctuation)
						p2 += 2
					}
					pos = p2
				}
			case '+':
				if (pos > 0 && p.source[pos-1] == '{') || (pos+1 <= end && p.source[pos+1] == '}') {
					pos = p.handleEmphasis(b, pos, end)
					continue
				} else {
					p.addEvent(pos, pos, EvStr)
					pos++
				}
			case '=':
				if (pos > 0 && p.source[pos-1] == '{') || (pos+1 <= end && p.source[pos+1] == '}') {
					pos = p.handleEmphasis(b, pos, end)
					continue
				} else {
					p.addEvent(pos, pos, EvStr)
					pos++
				}
			case '<':
				pos = p.handleAutolink(pos, end)
			case ':':
				pos = p.handleEmoji(pos, end)
			case '$':
				count := int32(0)
				for pos+count <= end && p.source[pos+count] == '$' {
					count++
				}
				if pos+count <= end && p.source[pos+count] == '`' {
					pos = p.handleMathVerbatim(pos, pos+count, end, textEvents, evIdx)
					for evIdx < len(textEvents) && pos > textEvents[evIdx].End {
						evIdx++
					}
					if evIdx < len(textEvents) && pos >= textEvents[evIdx].Start {
						end = textEvents[evIdx].End
						start = pos
					}
				} else {
					pos = p.handleMath(pos, end)
				}
			default:
				p.addEvent(pos, pos, EvStr)
				pos++
			}
			start = pos
		}
		evIdx++
	}

	for _, opener := range p.stack {
		if opener.MatchIndex != -1 && (opener.Char == '\'' || opener.Char == '"') {
			p.events[opener.MatchIndex].Type = EvSmartPunctuation
		}
	}
}

func (p *InlineParser) findClosingParenAhead(start int32, textEvents []Event, startEvIdx int) int32 {
	count := 0
	for j := startEvIdx; j < len(textEvents); j++ {
		ev := textEvents[j]
		if ev.Type == EvSoftBreak {
			continue
		}
		s := ev.Start
		if j == startEvIdx {
			s = start
		}
		for i := s; i <= ev.End; i++ {
			if p.source[i] == '(' {
				count++
			} else if p.source[i] == ')' {
				count--
				if count == 0 {
					return i
				}
			}
		}
	}
	return -1
}

func (p *InlineParser) findClosingBracketAhead(start int32, textEvents []Event, startEvIdx int) int32 {
	count := 0
	for j := startEvIdx; j < len(textEvents); j++ {
		ev := textEvents[j]
		if ev.Type == EvSoftBreak {
			continue
		}
		s := ev.Start
		if j == startEvIdx {
			s = start
		}
		for i := s; i <= ev.End; i++ {
			if p.source[i] == '[' {
				count++
			} else if p.source[i] == ']' {
				count--
				if count == 0 {
					return i
				}
			}
		}
	}
	return -1
}

func (p *InlineParser) handleCloseBracketAhead(pos int32, textEvents []Event, evIdx int, blockEnd int32) int32 {
	for i := len(p.stack) - 1; i >= 0; i-- {
		opener := p.stack[i]
		if opener.MatchIndex != -1 && opener.Char == '[' {
			next := pos + 1
			if next <= blockEnd && p.source[next] == '(' {
				closeIdx := p.findClosingParenAhead(next, textEvents, evIdx)
				if closeIdx != -1 {
					p.events[opener.MatchIndex].Type = opener.Type
					p.addEvent(next+1, closeIdx-1, EvOpenDestination)
					p.addEvent(next+1, closeIdx-1, EvCloseDestination)
					p.addEvent(pos, pos, closeTypeFor(opener.Type))
					p.stack = p.stack[:i]
					return closeIdx + 1
				}
			}

			if next <= blockEnd && p.source[next] == '[' {
				closeIdx := p.findClosingBracketAhead(next, textEvents, evIdx)
				if closeIdx != -1 {
					p.events[opener.MatchIndex].Type = opener.Type
					p.addEvent(next+1, closeIdx-1, EvOpenReference)
					p.addEvent(next+1, closeIdx-1, EvCloseReference)
					p.addEvent(pos, pos, closeTypeFor(opener.Type))
					p.stack = p.stack[:i]
					return closeIdx + 1
				}
			}

			if next <= blockEnd && p.source[next] == '{' {
				attrEvents, attrEndPos, ok := ParseAttributes(p.source, next, blockEnd+1)
				if ok {
					p.events[opener.MatchIndex].Type = EvOpenSpan
					p.addEvent(pos, pos, EvCloseSpan)
					p.addEvent(next, next, EvOpenInlineAttributes)
					p.events = append(p.events, attrEvents...)
					p.addEvent(attrEndPos, attrEndPos, EvCloseAttributes)
					p.stack = p.stack[:i]
					return attrEndPos + 1
				}
			}

			p.events[opener.MatchIndex].Type = EvStr
			p.addEvent(pos, pos, EvStr)
			p.stack = p.stack[:i]
			return pos + 1
		}
	}
	p.addEvent(pos, pos, EvStr)
	return pos + 1
}

func (p *InlineParser) handleMathVerbatim(dollarPos, backtickPos, end int32, textEvents []Event, startEvIdx int) int32 {
	count := int32(0)
	pos := backtickPos
	ev := textEvents[startEvIdx]
	for pos <= ev.End && p.source[pos] == '`' {
		count++
		pos++
	}

	contentStart := pos

	for j := startEvIdx; j < len(textEvents); j++ {
		tev := textEvents[j]
		if tev.Type == EvSoftBreak {
			continue
		}

		s := tev.Start
		if j == startEvIdx {
			s = pos
		}

		for i := s; i <= tev.End; i++ {
			if p.source[i] == '`' {
				c := int32(0)
				for i+c <= tev.End && p.source[i+c] == '`' {
					c++
				}
				if c == count {
					contentEnd := i - 1
					dollarCount := backtickPos - dollarPos
					etype := EvInlineMath
					if dollarCount == 2 {
						etype = EvDisplayMath
					}
					p.addEvent(contentStart, contentEnd, etype)
					return i + count
				}
				i += c - 1
			}
		}
	}

	lastEv := textEvents[len(textEvents)-1]
	dollarCount := backtickPos - dollarPos
	etype := EvInlineMath
	if dollarCount == 2 {
		etype = EvDisplayMath
	}
	p.addEvent(contentStart, lastEv.End, etype)
	return lastEv.End + 1
}

func (p *InlineParser) handleVerbatimAhead(pos int32, textEvents []Event, startEvIdx int) int32 {
	count := int32(0)

	ev := textEvents[startEvIdx]
	for pos <= ev.End && p.source[pos] == '`' {
		count++
		pos++
	}

	contentStart := pos

	for j := startEvIdx; j < len(textEvents); j++ {
		tev := textEvents[j]
		if tev.Type == EvSoftBreak {
			continue
		}

		s := tev.Start
		if j == startEvIdx {
			s = pos
		}

		for i := s; i <= tev.End; i++ {
			if p.source[i] == '`' {
				c := int32(0)
				for i+c <= tev.End && p.source[i+c] == '`' {
					c++
				}
				if c == count {
					contentEnd := i - 1

					if count == 1 && contentEnd-contentStart+1 >= 2 && p.source[contentStart] == ' ' && p.source[contentEnd] == ' ' {
						allSpaces := true
						for k := contentStart; k <= contentEnd; k++ {
							if p.source[k] != ' ' {
								allSpaces = false
								break
							}
						}
						if !allSpaces {
							contentStart++
							contentEnd--
						}
					}

					p.addEvent(contentStart, contentEnd, EvOpenVerbatim)
					p.addEvent(contentStart, contentEnd, EvCloseVerbatim)
					return i + count
				}
				i += c - 1
			}
		}
	}

	lastEv := textEvents[len(textEvents)-1]
	p.addEvent(contentStart, lastEv.End, EvOpenVerbatim)
	p.addEvent(contentStart, lastEv.End, EvCloseVerbatim)
	return lastEv.End + 1
}

func (p *InlineParser) handleAttributeAttachment(braceStart, braceEnd int32, attrEvents []Event) {
	p.addEvent(braceStart, braceStart, EvOpenInlineAttributes)
	p.events = append(p.events, attrEvents...)
	p.addEvent(braceEnd, braceEnd, EvCloseAttributes)
}

func isSpecial(b byte) bool {
	switch b {
	case '\\', '`', '_', '*', '~', '^', '$', '[', ']', '!', '<', '.', '-', ':', '"', '\'', '+', '=':
		return true
	}
	return false
}

func closeTypeFor(openType EventType) EventType {
	if openType == EvOpenLinkText {
		return EvCloseLinkText
	}
	return EvCloseImageText
}

func (p *InlineParser) addEvent(start, end int32, etype EventType) int32 {
	p.events = append(p.events, Event{Start: start, End: end, Type: etype})
	return int32(len(p.events) - 1)
}

func (p *InlineParser) addOpener(char byte, pos int32, etype EventType, explicit bool) {
	idx := p.addEvent(pos, pos, etype)
	p.stack = append(p.stack, Opener{
		MatchIndex: idx,
		Start:      pos,
		Type:       etype,
		Char:       char,
		Explicit:   explicit,
	})
}

func (p *InlineParser) handleEmphasis(b byte, pos int32, end int32) int32 {
	lastMatch := Event{Type: EvNone}
	if len(p.events) > 0 {
		lastMatch = p.events[len(p.events)-1]
	}

	isExplicitOpen := lastMatch.Type == EvOpenInlineAttributes && lastMatch.Start == pos-1
	isExplicitClose := pos+1 <= end && p.source[pos+1] == '}'

	canOpen := pos+1 <= end && !isSpace(p.source[pos+1])
	canClose := pos-1 >= 0 && !isSpace(p.source[pos-1])

	if isExplicitOpen {
		canOpen = true
		canClose = false
	}
	if isExplicitClose {
		canClose = true
		canOpen = false
	}

	if canClose {
		var openerIdx = -1
		for i := len(p.stack) - 1; i >= 0; i-- {
			if p.stack[i].MatchIndex != -1 && p.stack[i].Char == b && p.stack[i].Explicit == isExplicitClose && p.stack[i].Type != EvOpenLinkText && p.stack[i].Type != EvOpenImageText {
				openerIdx = i
				break
			}
		}

		if openerIdx != -1 {
			opener := p.stack[openerIdx]
			if opener.Start != pos-1 {
				p.handleCloseEmphasisAt(openerIdx, pos, isExplicitClose)
				if isExplicitClose {
					return pos + 2
				}
				return pos + 1
			}
		}
	}

	if canOpen {
		startPos := pos
		if isExplicitOpen {
			startPos = pos - 1
			p.events = p.events[:len(p.events)-1]
			p.addOpener(b, startPos, EvStr, isExplicitOpen)
			p.addEvent(pos, pos, EvStr)
		} else {
			p.addOpener(b, startPos, EvStr, isExplicitOpen)
		}
		return pos + 1
	} else {
		p.addEvent(pos, pos, EvStr)
		if isExplicitClose {
			p.addEvent(pos+1, pos+1, EvStr)
			return pos + 2
		}
		return pos + 1
	}
}

func (p *InlineParser) handleCloseEmphasisAt(openerIdx int, pos int32, explicit bool) {
	opener := p.stack[openerIdx]
	var b = opener.Char
	var openType, closeType EventType
	switch b {
	case '_':
		openType, closeType = EvOpenEmph, EvCloseEmph
	case '*':
		openType, closeType = EvOpenStrong, EvCloseStrong
	case '~':
		openType, closeType = EvOpenSubscript, EvCloseSubscript
	case '^':
		openType, closeType = EvOpenSuperscript, EvCloseSuperscript
	case '-':
		openType, closeType = EvOpenDelete, EvCloseDelete
	case '+':
		openType, closeType = EvOpenInsert, EvCloseInsert
	case '=':
		openType, closeType = EvOpenMark, EvCloseMark
	case '\'':
		openType, closeType = EvOpenSingleQuoted, EvCloseSingleQuoted
	case '"':
		openType, closeType = EvOpenDoubleQuoted, EvCloseDoubleQuoted
	}
	p.events[opener.MatchIndex].Type = openType
	p.events[opener.MatchIndex].Start = opener.Start

	if opener.Explicit {
		if opener.MatchIndex+1 < int32(len(p.events)) && p.events[opener.MatchIndex+1].Start == opener.Start+1 && p.events[opener.MatchIndex+1].Type == EvStr {
			p.events[opener.MatchIndex+1].Type = EvNone
		}
	}

	endPos := pos
	if explicit {
		endPos = pos + 1
	}
	p.addEvent(pos, endPos, closeType)

	p.clearOpeners(opener.Start, pos)
	p.stack = p.stack[:openerIdx]
}

func (p *InlineParser) clearOpeners(start, end int32) {
	for i := range p.stack {
		if p.stack[i].Start > start && p.stack[i].Start < end {
			if p.stack[i].MatchIndex != -1 {
				p.events[p.stack[i].MatchIndex].Type = EvStr
				p.stack[i].MatchIndex = -1
			}
		}
	}
}

func (p *InlineParser) handleMath(pos, end int32) int32 {
	start := pos
	pos++
	count := int32(1)
	if pos <= end && p.source[pos] == '$' {
		count = 2
		pos++
	}
	for pos <= end {
		if p.source[pos] == '$' {
			c := int32(0)
			for pos <= end && p.source[pos] == '$' {
				c++
				pos++
			}
			if c == count {
				etype := EvInlineMath
				if count == 2 {
					etype = EvDisplayMath
				}
				p.addEvent(start+count, pos-count-1, etype)
				return pos
			}
		} else {
			pos++
		}
	}
	p.addEvent(start, start+count-1, EvStr)
	return start + count
}

func (p *InlineParser) handleAutolink(pos, end int32) int32 {
	start := pos
	hasAt := false
	hasSlash := false
	for pos <= end && p.source[pos] != '>' && p.source[pos] != ' ' {
		if p.source[pos] == '@' {
			hasAt = true
		} else if p.source[pos] == '/' {
			hasSlash = true
		}
		pos++
	}
	if pos <= end && p.source[pos] == '>' {
		etype := EvUrl
		if hasAt && !hasSlash {
			etype = EvEmail
		}
		p.addEvent(start+1, pos-1, etype)
		return pos + 1
	}
	p.addEvent(start, start, EvStr)
	return start + 1
}

func (p *InlineParser) handleEmoji(pos, end int32) int32 {
	start := pos
	pos++
	for pos <= end && isAlphaLower(p.source[pos]) {
		pos++
	}
	if pos <= end && p.source[pos] == ':' {
		p.addEvent(start+1, pos-1, EvSymb)
		return pos + 1
	}
	p.addEvent(start, start, EvStr)
	return start + 1
}

func (p *InlineParser) handleDoubleQuote(pos, end int32) {
	for i := len(p.stack) - 1; i >= 0; i-- {
		opener := p.stack[i]
		if opener.MatchIndex != -1 && opener.Char == '"' {
			p.events[opener.MatchIndex].Type = EvOpenDoubleQuoted
			p.addEvent(pos, pos, EvCloseDoubleQuoted)
			p.stack = p.stack[:i]
			return
		}
	}
	p.addOpener('"', pos, EvStr, false)
}

func (p *InlineParser) handleSingleQuote(pos, end int32) int32 {
	isExplicitClose := pos+1 <= end && p.source[pos+1] == '}'

	if isExplicitClose {
		for i := len(p.stack) - 1; i >= 0; i-- {
			if p.stack[i].MatchIndex != -1 && p.stack[i].Char == '\'' && p.stack[i].Explicit {
				opener := p.stack[i]
				p.events[opener.MatchIndex].Type = EvOpenSingleQuoted
				p.addEvent(pos, pos, EvCloseSingleQuoted)
				if opener.MatchIndex+1 < int32(len(p.events)) && p.events[opener.MatchIndex+1].Start == opener.Start+1 && p.events[opener.MatchIndex+1].Type == EvStr {
					p.events[opener.MatchIndex+1].Type = EvNone
				}
				p.stack = p.stack[:i]
				return pos + 2
			}
		}
	}

	prevIsCloser := pos > 0 && (isAlnum(p.source[pos-1]) || isClosingPunct(p.source[pos-1]))
	nextDigit := pos+1 <= end && isDigit(p.source[pos+1])

	canClose := pos > 0 && !isSpace(p.source[pos-1])
	canOpen := pos+1 <= end && !isSpace(p.source[pos+1]) &&
		(pos == 0 || isOpeningContext(p.source[pos-1]))

	if prevIsCloser || nextDigit {
		for i := len(p.stack) - 1; i >= 0; i-- {
			if p.stack[i].MatchIndex != -1 && p.stack[i].Char == '\'' {
				p.events[p.stack[i].MatchIndex].Type = EvOpenSingleQuoted
				p.addEvent(pos, pos, EvCloseSingleQuoted)
				p.stack = p.stack[:i]
				return pos + 1
			}
		}
		p.addEvent(pos, pos, EvSmartPunctuation)
		return pos + 1
	}

	if canClose && !canOpen {
		for i := len(p.stack) - 1; i >= 0; i-- {
			opener := p.stack[i]
			if opener.MatchIndex != -1 && opener.Char == '\'' {
				p.events[opener.MatchIndex].Type = EvOpenSingleQuoted
				p.addEvent(pos, pos, EvCloseSingleQuoted)
				p.stack = p.stack[:i]
				return pos + 1
			}
		}
	}

	if canOpen {
		p.addOpener('\'', pos, EvStr, false)
	} else {
		p.addEvent(pos, pos, EvSmartPunctuation)
	}
	return pos + 1
}

func isAlnum(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

func isClosingPunct(b byte) bool {
	return b == ')' || b == ']' || b == '}' ||
		b == '.' || b == ',' || b == ';' || b == ':' ||
		b == '!' || b == '?'
}

func isOpeningContext(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' ||
		b == '"' || b == '\'' || b == '-' || b == '(' || b == '['
}

func isPunctuation(b byte) bool {
	switch b {
	case '!', '"', '#', '$', '%', '&', '\'', '(', ')', '*', '+', ',', '-', '.', '/', ':', ';', '<', '=', '>', '?', '@', '[', '\\', ']', '^', '_', '`', '{', '|', '}', '~':
		return true
	}
	return false
}
