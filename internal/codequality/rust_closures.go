package codequality

import "strings"

func rustClosures(m *metrics, f File, text string, ends map[int]int) {
	for at := 0; at < len(text); at++ {
		if m.cancelled() {
			return
		}
		if text[at] != '|' || !rustClosurePrefix(text, at) {
			continue
		}
		close := at + 1
		for close < len(text) && text[close] != '|' {
			if end, ok := ends[close]; ok {
				close = end
			}
			close++
		}
		if close >= len(text) {
			continue
		}
		body := scriptSpace(text, close+1)
		if strings.HasPrefix(text[body:], "->") {
			for body < len(text) && text[body] != '{' && text[body] != ';' {
				body++
			}
		}
		if body < len(text) && text[body] == '{' {
			if end, ok := ends[body]; ok {
				m.scriptFunctionFinding(f, text, "closure", at, body, end, ends)
			}
		} else if end := scriptExpressionEnd(f.Text, text, body, ends); end >= body {
			m.scriptFunctionFinding(f, text, "closure", at, body, end, ends)
		}
		at = close
	}
}

func rustClosurePrefix(text string, at int) bool {
	for at--; at >= 0 && strings.ContainsRune(" \t\n\r", rune(text[at])); at-- {
	}
	if at < 0 || strings.ContainsRune("=(:,;{", rune(text[at])) {
		return true
	}
	end := at + 1
	for at >= 0 && scriptWord(text[at]) {
		at--
	}
	switch text[at+1 : end] {
	case "move", "async", "return", "break":
		return true
	}
	return false
}
