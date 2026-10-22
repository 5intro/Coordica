package codequality

import "strings"

// Shell '#' is a comment only at a token boundary, never the trim operator
// inside ${...}. Keep expansion braces visible for the structural scan.
func maskedShell(text string) string {
	out := []byte(text)
	depth := 0
	for at := 0; at < len(text); at++ {
		if text[at] == '\\' {
			at++
			continue
		}
		if text[at] == '\'' || text[at] == '"' || text[at] == '`' {
			at = scriptQuoteEnd(text, at) - 1
			continue
		}
		if strings.HasPrefix(text[at:], "${") {
			depth++
			at++
		} else if text[at] == '}' && depth > 0 {
			depth--
		} else if text[at] == '#' {
			if depth > 0 || at > 0 && !strings.ContainsRune(" \t\r\n;|&()", rune(text[at-1])) {
				out[at] = '+'
			} else {
				for at < len(text) && text[at] != '\n' {
					at++
				}
			}
		}
	}
	return maskedSource(string(out), true, map[int]int{})
}

func maskedRuby(text string) string {
	out := []byte(text)
	for at := 0; at < len(text); {
		start, end := at, at
		switch {
		case text[at] == '#':
			for at < len(text) && text[at] != '\n' {
				at++
			}
			end = at
		case text[at] == '\'' || text[at] == '"' || text[at] == '`':
			end = scriptQuoteEnd(text, at)
		case text[at] == '/' && scriptRegexEnd(text, at) > at:
			end = scriptRegexEnd(text, at)
		case text[at] == '%' && at+2 < len(text) && strings.ContainsRune("qQrwWxsiI", rune(text[at+1])):
			end = rubyPercentEnd(text, at+2)
		case text[at] == ':' && at+1 < len(text) && scriptWord(text[at+1]) && (at == 0 || text[at-1] != ':'):
			end = at + 2
			for end < len(text) && scriptWord(text[end]) {
				end++
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

func rubyPercentEnd(text string, at int) int {
	open, close := text[at], text[at]
	if index := strings.IndexByte("([{<", open); index >= 0 {
		close = ")]}>"[index]
	}
	if scriptWord(open) || strings.ContainsRune(" \t\n", rune(open)) {
		return at
	}
	depth := 1
	for end := at + 1; end < len(text); end++ {
		if text[end] == '\\' {
			end++
		} else if text[end] == close {
			depth--
			if depth == 0 {
				return end + 1
			}
		} else if open != close && text[end] == open {
			depth++
		}
	}
	return len(text)
}
