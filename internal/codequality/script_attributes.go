package codequality

import "strings"

func scriptTagEnd(text string, at int) int {
	quote := byte(0)
	for ; at < len(text); at++ {
		if quote != 0 {
			if text[at] == quote {
				quote = 0
			}
		} else if text[at] == '\'' || text[at] == '"' {
			quote = text[at]
		} else if text[at] == '>' {
			return at + 1
		}
	}
	return -1
}

func inertScriptAttributes(text string) bool {
	for at := 0; at < len(text); {
		for at < len(text) && strings.ContainsRune(" \t\n\r\f/>", rune(text[at])) {
			at++
		}
		start := at
		for at < len(text) && !strings.ContainsRune(" \t\n\r\f=/>", rune(text[at])) {
			at++
		}
		name := text[start:at]
		at = htmlAttributeSpace(text, at)
		value := ""
		if at < len(text) && text[at] == '=' {
			at = htmlAttributeSpace(text, at+1)
			start = at
			if at < len(text) && (text[at] == '\'' || text[at] == '"') {
				quote := text[at]
				start, at = at+1, at+1
				for at < len(text) && text[at] != quote {
					at++
				}
				value = text[start:at]
				at = min(len(text), at+1)
			} else {
				for at < len(text) && !strings.ContainsRune(" \t\n\r\f>", rune(text[at])) {
					at++
				}
				value = text[start:at]
			}
		}
		if strings.EqualFold(name, "type") {
			switch strings.ToLower(strings.TrimSpace(value)) {
			case "application/json", "application/ld+json", "importmap", "speculationrules":
				return true
			}
			return false // HTML honors the first type attribute, including empty.
		}
		if start == at {
			at++
		}
	}
	return false
}

func htmlAttributeSpace(text string, at int) int {
	for at < len(text) && strings.ContainsRune(" \t\n\r\f", rune(text[at])) {
		at++
	}
	return at
}
