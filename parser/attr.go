package parser

import (
	. "github.com/nein-ar/dejot/ast"
)

func isKeyStartChar(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || b == '_' || b == '-' || b == ':'
}

func isKeyChar(b byte) bool {
	return isKeyStartChar(b) || (b >= '0' && b <= '9')
}

func isIDChar(b byte) bool {
	return isKeyChar(b)
}

func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\r' || b == '\n'
}

func ParseAttributes(source []byte, start, end int32) ([]Event, int32, bool) {
	if start >= end || source[start] != '{' {
		return nil, start, false
	}

	pos := start + 1
	var events []Event

	for pos < end {
		b := source[pos]
		if isSpace(b) {
			pos++
			continue
		}

		if b == '}' {
			return events, pos, true
		}

		if b == '#' {
			pos++
			idStart := pos
			for pos < end && isIDChar(source[pos]) {
				pos++
			}
			if pos == idStart {
				return nil, start, false
			}
			events = append(events, Event{Start: idStart, End: pos - 1, Type: EvAttrIdMarker})
			continue
		}

		if b == '.' {
			pos++
			classStart := pos
			for pos < end && isKeyChar(source[pos]) {
				pos++
			}
			if pos == classStart {
				return nil, start, false
			}
			events = append(events, Event{Start: classStart, End: pos - 1, Type: EvAttrClassMarker})
			continue
		}

		if b == '%' {
			pos++
			commentStart := pos
			for pos < end && source[pos] != '%' && source[pos] != '}' {
				pos++
			}
			if pos < end && source[pos] == '%' {
				events = append(events, Event{Start: commentStart, End: pos - 1, Type: EvComment})
				pos++
			}
			continue
		}

		if b == '=' {
			eqPos := pos
			pos++
			keyStart := pos
			for pos < end && source[pos] != '}' && !isSpace(source[pos]) {
				pos++
			}
			keyEnd := pos - 1
			if keyEnd >= keyStart {
				events = append(events, Event{Type: EvAttrKey, Start: eqPos, End: keyEnd})
			}
			continue
		}

		if isKeyStartChar(b) {
			keyStart := pos
			for pos < end && isKeyChar(source[pos]) {
				pos++
			}
			keyEnd := pos - 1

			for pos < end && isSpace(source[pos]) {
				pos++
			}

			if pos < end && source[pos] == '=' {
				events = append(events, Event{Start: keyStart, End: keyEnd, Type: EvAttrKey})
				events = append(events, Event{Start: pos, End: pos, Type: EvAttrEqualMarker})
				pos++

				for pos < end && isSpace(source[pos]) {
					pos++
				}

				if pos < end && source[pos] == '"' {
					events = append(events, Event{Start: pos, End: pos, Type: EvAttrQuoteMarker})
					pos++
					valStart := pos
					valEnd := valStart
					for pos < end {
						if source[pos] == '\\' && pos+1 < end {
							pos += 2
						} else if source[pos] == '"' {
							valEnd = pos
							break
						} else {
							pos++
						}
					}

					if pos < end && source[pos] == '"' {
						events = append(events, Event{Start: valStart, End: valEnd - 1, Type: EvAttrValue})
						events = append(events, Event{Start: pos, End: pos, Type: EvAttrQuoteMarker})
						pos++
					} else {
						return nil, start, false
					}
				} else {
					valStart := pos
					for pos < end && isKeyChar(source[pos]) {
						pos++
					}
					if pos == valStart {
						return nil, start, false
					}
					events = append(events, Event{Start: valStart, End: pos - 1, Type: EvAttrValue})
				}
			} else {
				return nil, start, false
			}
			continue
		}

		return nil, start, false
	}

	return nil, start, false
}
