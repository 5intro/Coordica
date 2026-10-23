package codequality

import "strings"

func maskedCppRawStrings(text string) string {
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
			if end := strings.Index(text[at+2:], "*/"); end >= 0 {
				at += end + 3
			} else {
				return string(out)
			}
		case strings.HasPrefix(text[at:], `R"`):
			if end := cppRawStringEnd(text, at); end > at {
				scriptBlank(out, at, end)
				at = end - 1
			}
		case text[at] == '\'' || text[at] == '"':
			at = scriptQuoteEnd(text, at) - 1
		}
	}
	return string(out)
}

func cppRawStringEnd(text string, at int) int {
	start := at + 2
	open := strings.IndexByte(text[start:], '(')
	if open < 0 || open > 16 {
		return at
	}
	delimiter := text[start : start+open]
	if strings.ContainsAny(delimiter, " \t\n\r\\)") {
		return at
	}
	terminal := ")" + delimiter + `"`
	body := start + open + 1
	if end := strings.Index(text[body:], terminal); end >= 0 {
		return body + end + len(terminal)
	}
	return len(text)
}
