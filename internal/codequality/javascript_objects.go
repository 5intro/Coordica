package codequality

import "strings"

func scriptObjectPrefix(text string, at int) bool {
	i := at - 1
	for i >= 0 {
		if strings.ContainsRune(" \t\r\n", rune(text[i])) || text[i] == '*' {
			i--
			continue
		}
		if scriptWord(text[i]) {
			end := i + 1
			for i >= 0 && scriptWord(text[i]) {
				i--
			}
			word := text[i+1 : end]
			if word == "get" || word == "set" || word == "async" {
				continue
			}
			return false
		}
		return text[i] == '{' || text[i] == ','
	}
	return false
}

func (m *metrics) scriptObjectMethods(f File, text string, ends map[int]int, seen map[int]bool) {
	for _, match := range scriptMethodMatches(m.ctx, f.Text, text, ends) {
		if m.cancelled() {
			return
		}
		name := f.Text[match[2]:match[3]]
		switch name {
		case "if", "for", "while", "switch", "catch", "with", "function":
			continue
		}
		if !scriptObjectPrefix(text, match[0]) {
			continue
		}
		body := scriptFunctionBody(text, match[3], ends)
		if close, ok := ends[body]; ok && !seen[body] {
			m.scriptFunctionFinding(f, text, "object."+name, match[0], body, close, ends)
			seen[body] = true
		}
	}
}
