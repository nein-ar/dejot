package parser

import (
	. "github.com/nein-ar/dejot/ast"
)

func isThematicBreak(source []byte, start, end int32) bool {
	count := 0
	for i := start; i < end; i++ {
		b := source[i]
		if b == '-' || b == '*' {
			count++
		} else if b == ' ' || b == '\t' {
			continue
		} else {
			return false
		}
	}
	return count >= 3
}

func parseListMarker(source []byte, pos, end int32) (int32, ContainerData, bool) {
	if pos >= end {
		return pos, ContainerData{}, false
	}

	startPos := pos
	b := source[pos]

	if b == '-' || b == '+' || b == '*' {
		return checkListItemEnd(source, startPos, pos, end, b, false, 0, 0)
	}

	if b == ':' {
		return checkListItemEnd(source, startPos, pos, end, ':', false, 0, 0)
	}

	if isDigit(b) {
		p := pos
		for p < end && isDigit(source[p]) {
			p++
		}
		if p < end && (source[p] == '.' || source[p] == ')') {
			num := parseDecimal(source[pos:p])
			return checkListItemEnd(source, startPos, p, end, '1', true, num, source[p])
		}
	}

	if isRomanLowerStart(b) {
		p := pos
		for p < end && isRomanLower(source[p]) {
			p++
		}
		if p < end && (source[p] == '.' || source[p] == ')') {
			num := parseRoman(source[pos:p])
			newPos, data, ok := checkListItemEnd(source, startPos, p, end, 'i', true, num, source[p])
			if ok && p == pos+1 {
				data.MarkerAmbiguous = true
			}
			if ok {
				data.MarkerFirstChar = b
			}
			return newPos, data, ok
		}
	}

	if isAlphaLower(b) {
		if pos+1 < end && (source[pos+1] == '.' || source[pos+1] == ')') {
			num := int(b - 'a' + 1)
			newPos, data, ok := checkListItemEnd(source, startPos, pos+1, end, 'a', true, num, source[pos+1])
			if ok {
				data.MarkerFirstChar = b
			}
			return newPos, data, ok
		}
	}

	if isRomanUpperStart(b) {
		p := pos
		for p < end && isRomanUpper(source[p]) {
			p++
		}
		if p < end && (source[p] == '.' || source[p] == ')') {
			num := parseRoman(source[pos:p])
			newPos, data, ok := checkListItemEnd(source, startPos, p, end, 'I', true, num, source[p])
			if ok && p == pos+1 {
				data.MarkerAmbiguous = true
			}
			if ok {
				data.MarkerFirstChar = b
			}
			return newPos, data, ok
		}
	}

	if isAlphaUpper(b) {
		if pos+1 < end && (source[pos+1] == '.' || source[pos+1] == ')') {
			num := int(b - 'A' + 1)
			newPos, data, ok := checkListItemEnd(source, startPos, pos+1, end, 'A', true, num, source[pos+1])
			if ok {
				data.MarkerFirstChar = b
			}
			return newPos, data, ok
		}
	}

	if b == '(' {
		p := pos + 1
		if p < end {
			b2 := source[p]

			if isDigit(b2) {
				start := p
				for p < end && isDigit(source[p]) {
					p++
				}
				if p < end && source[p] == ')' {
					num := parseDecimal(source[start:p])
					newPos, data, ok := checkListItemEnd(source, startPos, p, end, '1', true, num, 'X')
					if ok {
						data.MarkerFirstChar = b2
					}
					return newPos, data, ok
				}
			}

			if isAlphaLower(b2) && p+1 < end && source[p+1] == ')' {
				num := int(b2 - 'a' + 1)
				newPos, data, ok := checkListItemEnd(source, startPos, p+1, end, 'a', true, num, 'X')
				if ok {
					data.MarkerFirstChar = b2
					if isRomanLowerStart(b2) {
						data.MarkerAmbiguous = true
					}
				}
				return newPos, data, ok
			}
			if isAlphaUpper(b2) && p+1 < end && source[p+1] == ')' {
				num := int(b2 - 'A' + 1)
				newPos, data, ok := checkListItemEnd(source, startPos, p+1, end, 'A', true, num, 'X')
				if ok {
					data.MarkerFirstChar = b2
					if isRomanUpperStart(b2) {
						data.MarkerAmbiguous = true
					}
				}
				return newPos, data, ok
			}

			if isRomanLowerStart(b2) {
				start := p
				for p < end && isRomanLower(source[p]) {
					p++
				}
				if p < end && source[p] == ')' {
					if p > start+1 {
						num := parseRoman(source[start:p])
						newPos, data, ok := checkListItemEnd(source, startPos, p, end, 'i', true, num, 'X')
						if ok {
							data.MarkerFirstChar = b2
						}
						return newPos, data, ok
					}
				}
			}

			if isRomanUpperStart(b2) {
				start := p
				for p < end && isRomanUpper(source[p]) {
					p++
				}
				if p < end && source[p] == ')' {
					if p > start+1 {
						num := parseRoman(source[start:p])
						newPos, data, ok := checkListItemEnd(source, startPos, p, end, 'I', true, num, 'X')
						if ok {
							data.MarkerFirstChar = b2
						}
						return newPos, data, ok
					}
				}
			}
		}
	}

	return startPos, ContainerData{}, false
}

func checkListItemEnd(source []byte, start, pos, end int32, style byte, ordered bool, num int, endChar byte) (int32, ContainerData, bool) {
	if pos+1 == end || source[pos+1] == ' ' || source[pos+1] == '\t' {
		newPos := pos + 1
		for newPos < end && (source[newPos] == ' ' || source[newPos] == '\t') {
			newPos++
		}
		indent := newPos - start

		checked := false
		isTask := false
		if !ordered && (style == '-' || style == '+' || style == '*') && newPos+3 <= end && source[newPos] == '[' {
			if source[newPos+1] == ' ' && source[newPos+2] == ']' {
				isTask = true
				newPos += 3
			} else if (source[newPos+1] == 'x' || source[newPos+1] == 'X') && source[newPos+2] == ']' {
				isTask = true
				checked = true
				newPos += 3
			}

			if isTask {
				if newPos == end || source[newPos] == ' ' || source[newPos] == '\t' {
					for newPos < end && (source[newPos] == ' ' || source[newPos] == '\t') {
						newPos++
					}
					indent = newPos - start
				} else {
					isTask = false
					checked = false
					newPos = pos + 1
					for newPos < end && (source[newPos] == ' ' || source[newPos] == '\t') {
						newPos++
					}
					indent = newPos - start
				}
			}
		}

		if !ordered && indent == 1 && newPos < end {
			return start, ContainerData{}, false
		}

		return newPos, ContainerData{Marker: style, MarkerEnd: endChar, Ordered: ordered, IsTask: isTask, Checked: checked, Tight: true, NextNumber: num}, true
	}
	return start, ContainerData{}, false
}

func parseDecimal(b []byte) int {
	res := 0
	for _, c := range b {
		res = res*10 + int(c-'0')
	}
	return res
}

func parseRoman(b []byte) int {
	val := func(c byte) int {
		switch c {
		case 'i', 'I':
			return 1
		case 'v', 'V':
			return 5
		case 'x', 'X':
			return 10
		case 'l', 'L':
			return 50
		case 'c', 'C':
			return 100
		case 'd', 'D':
			return 500
		case 'm', 'M':
			return 1000
		}
		return 0
	}
	res := 0
	for i := 0; i < len(b); i++ {
		v := val(b[i])
		if i+1 < len(b) && val(b[i+1]) > v {
			res -= v
		} else {
			res += v
		}
	}
	return res
}

func isDigit(b byte) bool {
	return b >= '0' && b <= '9'
}

func isAlphaLower(b byte) bool {
	return b >= 'a' && b <= 'z'
}

func isAlphaUpper(b byte) bool {
	return b >= 'A' && b <= 'Z'
}

func isRomanLowerStart(b byte) bool {
	switch b {
	case 'i', 'v', 'x', 'l', 'c', 'd', 'm':
		return true
	}
	return false
}

func isRomanLower(b byte) bool {
	return isRomanLowerStart(b)
}

func isRomanUpperStart(b byte) bool {
	switch b {
	case 'I', 'V', 'X', 'L', 'C', 'D', 'M':
		return true
	}
	return false
}

func isRomanUpper(b byte) bool {
	return isRomanUpperStart(b)
}
