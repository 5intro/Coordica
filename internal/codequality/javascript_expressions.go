package codequality

import "strings"

func (m *metrics) scriptArrows(f File, text string, ends map[int]int) {
	starts := map[int]int{}
	for open, close := range ends {
		starts[close] = open
	}
	for at := 0; at < len(text)-1; at++ {
		if !strings.HasPrefix(text[at:], "=>") {
			continue
		}
		start := scriptArrowStart(text, at, starts)
		body := scriptSpace(text, at+2)
		if body < len(text) && text[body] == '{' {
			if close, ok := ends[body]; ok {
				m.scriptFunctionFinding(f, text, "arrow", start, body, close, ends)
			}
			continue
		}
		end := scriptExpressionEnd(f.Text, text, body, ends)
		if end >= body && body < len(text) {
			m.scriptFunctionFinding(f, text, "arrow", start, body, end, ends)
		}
	}
}

func scriptArrowStart(text string, at int, starts map[int]int) int {
	i := at - 1
	for i >= 0 && strings.ContainsRune(" \t\r\n", rune(text[i])) {
		i--
	}
	if open, ok := starts[i]; ok && text[open] == '(' {
		return open
	}
	for i >= 0 && scriptWord(text[i]) {
		i--
	}
	return i + 1
}

func scriptExpressionContinues(previous, text string, next int) bool {
	if next >= len(text) {
		return false
	}
	return strings.ContainsRune(".?+-*/%&|^<>=([", rune(text[next])) || scriptPrefix(previous) ||
		len(previous) == 1 && strings.ContainsRune("?.:+-*/%&|^<>=!~", rune(previous[0]))
}

func scriptExpressionEnd(original, text string, start int, ends map[int]int) int {
	last, previous := start-1, ""
	for i := start; i < len(text); {
		if next := scriptSpace(text, i); next != i {
			if strings.Contains(text[i:next], "\n") && !scriptExpressionContinues(previous, text, next) {
				return last
			}
			i = next
			continue
		}
		if strings.ContainsRune(";,)]}", rune(text[i])) {
			return last
		}
		if close, ok := ends[i]; ok {
			last, i, previous = close, close+1, "value"
			continue
		}
		if text[i] == '<' {
			if end := scriptJSXEnd(original, i, ends); end >= i {
				last, i, previous = end, end+1, "value"
				continue
			}
		}
		if text[i] == '0' && strings.ContainsRune("'\"`/", rune(original[i])) {
			end := scriptQuoteEnd(original, i)
			if original[i] == '/' {
				end = scriptRegexEnd(original, i)
			} else if original[i] == '`' {
				end = scriptTemplateEnd(original, i, ends)
			}
			last, i, previous = end-1, end, "value"
			continue
		}
		begin := i
		if scriptWord(text[i]) {
			for i < len(text) && scriptWord(text[i]) {
				i++
			}
		} else {
			i++
		}
		last, previous = i-1, text[begin:i]
	}
	return last
}

// JSX is one expression even when its child text spans many physical lines.
// Tag attributes may contain strings and balanced JavaScript expressions.
func scriptJSXEnd(text string, start int, ends map[int]int) int {
	depth := 0
	for i := start; i < len(text); {
		if text[i] == '{' {
			if end, ok := ends[i]; ok {
				i = end + 1
				continue
			}
		}
		if text[i] != '<' {
			i++
			continue
		}
		at := i + 1
		closing := at < len(text) && text[at] == '/'
		if closing {
			at++
		}
		if at >= len(text) || text[at] != '>' && !scriptWord(text[at]) {
			return -1
		}
		for at < len(text) && text[at] != '>' {
			if text[at] == '\'' || text[at] == '"' {
				at = scriptQuoteEnd(text, at)
			} else if end, ok := ends[at]; ok && text[at] == '{' {
				at = end + 1
			} else {
				at++
			}
		}
		if at == len(text) {
			return -1
		}
		if closing {
			depth--
		} else if !strings.HasSuffix(strings.TrimSpace(text[i:at]), "/") {
			depth++
		}
		if depth == 0 {
			return at
		}
		i = at + 1
	}
	return -1
}
