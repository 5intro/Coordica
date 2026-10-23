package codequality

import "strings"

func rubyBraceBlocks(m *metrics, f File, text string, pairs map[int]int) {
	for open, close := range pairs {
		if m.cancelled() {
			return
		}
		if text[open] != '{' || !rubyBlockPrefix(text, open) {
			continue
		}
		m.scriptFunctionFinding(f, text, "block", open, open, close, pairs)
	}
}

func rubyBlockPrefix(text string, open int) bool {
	at := open - 1
	for at >= 0 && strings.ContainsRune(" \t\r\n", rune(text[at])) {
		at--
	}
	if at < 0 || strings.ContainsRune("=(:,;[{?", rune(text[at])) {
		return false
	}
	end := at + 1
	for at >= 0 && scriptWord(text[at]) {
		at--
	}
	switch text[at+1 : end] {
	case "return", "break", "next", "yield", "raise", "then", "else":
		return false
	}
	return true
}
