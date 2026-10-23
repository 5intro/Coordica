package codequality

import (
	"regexp"
	"strings"
)

var kotlinFunction = regexp.MustCompile(`\bfun\s*(?:<[^{};]*>\s*)?` +
	`(?:[A-Za-z_][\w.<>?]*\s*\.\s*)?([A-Za-z_]\w*)`)
var kotlinMember = regexp.MustCompile(`^(?:(?:public|private|protected|internal|abstract|open|final|override|` +
	`suspend|inline|infix|operator|tailrec|external|expect|actual|const|lateinit)\s+)*` +
	`(?:fun|val|var|class|interface|object|typealias|init|constructor|companion)\b`)

func kotlinFunctionBlock(original, text, name string, start, at int, pairs map[int]int) (nativeBlock, bool) {
	close := nativeParameterClose(text, at, pairs)
	if close < 0 {
		return nativeBlock{}, false
	}
	for at = close + 1; at < len(text); at++ {
		switch text[at] {
		case '{':
			if end, ok := pairs[at]; ok {
				return nativeBlock{name, start, at, end}, true
			}
			return nativeBlock{}, false
		case '=':
			end := kotlinExpressionEnd(original, text, at+1, pairs)
			return nativeBlock{name, start, at, end}, true
		case ';', '}':
			return nativeBlock{name, start, -1, at - 1}, true
		case '\n':
			if next := scriptSpace(text, at+1); next == len(text) || kotlinMember.MatchString(text[next:]) {
				return nativeBlock{name, start, -1, at - 1}, true
			}
		}
		if end, ok := pairs[at]; ok {
			at = end
		}
	}
	return nativeBlock{name, start, -1, len(text) - 1}, true
}

func kotlinExpressionEnd(original, text string, start int, pairs map[int]int) int {
	end := len(text)
	for at := start; at < len(text); at++ {
		if text[at] == ';' || text[at] == '}' {
			end = at
			break
		}
		if text[at] == '\n' {
			if next := scriptSpace(text, at+1); next < len(text) && kotlinMember.MatchString(text[next:]) {
				end = at
				break
			}
		}
		if close, ok := pairs[at]; ok {
			at = close
		}
	}
	return start + len(strings.TrimRight(original[start:end], " \t\r\n")) - 1
}
