package codequality

import "strings"

func maskedRustRawStrings(text string) string {
	out := []byte(text)
	for at := 0; at < len(text); at++ {
		switch {
		case strings.HasPrefix(text[at:], "//"):
			if end := strings.IndexByte(text[at:], '\n'); end >= 0 {
				at += end
			} else {
				return string(out)
			}
		case strings.HasPrefix(text[at:], "/*"):
			// Rust block comments may nest.
			start, depth := at, 1
			at += 2
			for at < len(text) && depth > 0 {
				if strings.HasPrefix(text[at:], "/*") {
					depth++
					at += 2
				} else if strings.HasPrefix(text[at:], "*/") {
					depth--
					at += 2
				} else {
					at++
				}
			}
			scriptBlank(out, start, at)
			at--
		case text[at] == 'r' && (at == 0 || !scriptWord(text[at-1]) || (text[at-1] == 'b' || text[at-1] == 'c') &&
			(at < 2 || !scriptWord(text[at-2]))):
			if end := rustRawStringEnd(text, at); end > at {
				scriptBlank(out, at, end)
				at = end - 1
			}
		case text[at] == '"':
			at = scriptQuoteEnd(text, at) - 1
		case text[at] == '\'':
			// Skip actual character literals without treating a lifetime as a string.
			if match := rustLifetime.FindStringIndex(text[at:]); match != nil && match[0] == 0 &&
				(at+match[1] == len(text) || text[at+match[1]] != '\'') {
				continue
			}
			end := scriptQuoteEnd(text, at)
			if !strings.ContainsAny(text[at:end], "\r\n") {
				at = end - 1
			}
		}
	}
	return string(out)
}

func rustRawStringEnd(text string, at int) int {
	quote := at + 1
	for quote < len(text) && text[quote] == '#' {
		quote++
	}
	if quote >= len(text) || text[quote] != '"' || quote-at-1 > 255 {
		return at
	}
	terminal := `"` + text[at+1:quote]
	if end := strings.Index(text[quote+1:], terminal); end >= 0 {
		return quote + 1 + end + len(terminal)
	}
	return len(text)
}
