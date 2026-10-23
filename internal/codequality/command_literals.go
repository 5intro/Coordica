package codequality

import "strings"

// Shell '#' is a comment only at a token boundary, never the trim operator
// inside ${...}. Keep expansion braces visible for the structural scan.
func maskedShell(text string) string {
	return shellExecutableText(text)
}

func maskedRuby(text string) string {
	return maskedRubyLiterals(text, nil)
}

func maskedRubyLiterals(text string, literals map[int]int) string {
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
			if text[start] != '#' {
				out[start] = '0'
				if literals != nil {
					literals[start] = end - 1
				}
			}
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
