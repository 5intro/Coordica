package codequality

import "strings"

func maskedHashComments(text string, powerShell bool) string {
	out := []byte(text)
	for at := 0; at < len(text); {
		start, end := at, at
		switch {
		case powerShell && text[at] == '`':
			end = min(len(text), at+2)
		case powerShell && at+1 < len(text) && text[at] == '@' && strings.ContainsRune("'\"", rune(text[at+1])):
			end = powerHereEnd(text, at)
		case powerShell && strings.HasPrefix(text[at:], "<#"):
			end = powerBlockEnd(text, at)
		case !powerShell && strings.HasPrefix(text[at:], "/*"):
			end = at + 2
			for end < len(text) && !strings.HasPrefix(text[end:], "*/") {
				end++
			}
			end = min(len(text), end+2)
		case text[at] == '#' && (powerShell || !strings.HasPrefix(text[at:], "#[")) ||
			!powerShell && strings.HasPrefix(text[at:], "//"):
			end = at
			for end < len(text) && text[end] != '\n' {
				end++
			}
		case text[at] == '\'' || text[at] == '"' || !powerShell && text[at] == '`':
			end = scriptQuoteEnd(text, at)
			if powerShell {
				end = powerQuoteEnd(text, at)
			}
		}
		if end > start {
			scriptBlank(out, start, end)
			at = end
		} else {
			at++
		}
	}
	return string(out)
}

func powerBlockEnd(text string, at int) int {
	depth := 1
	for at += 2; at < len(text); at++ {
		if strings.HasPrefix(text[at:], "<#") {
			depth++
			at++
		} else if strings.HasPrefix(text[at:], "#>") {
			depth--
			at++
			if depth == 0 {
				return at + 1
			}
		}
	}
	return len(text)
}

func powerQuoteEnd(text string, start int) int {
	quote := text[start]
	for at := start + 1; at < len(text); at++ {
		if quote == '"' && text[at] == '`' {
			at++
		} else if text[at] == quote {
			if at+1 < len(text) && text[at+1] == quote {
				at++
			} else {
				return at + 1
			}
		}
	}
	return len(text)
}

func powerHereEnd(text string, start int) int {
	ending := string(text[start+1]) + "@"
	for at := start + 2; at < len(text); at++ {
		if text[at-1] == '\n' && strings.HasPrefix(text[at:], ending) {
			return at + 2
		}
	}
	return len(text)
}
