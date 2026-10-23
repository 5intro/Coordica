package codequality

import "strings"

// A constructor's initializer list belongs to its declaration, including
// braced members. Calls within it are not independent method declarations.
func cppInitializerBody(text string, at int, ends map[int]int) (int, bool) {
	at = scriptSpace(text, at)
	if at < len(text) && text[at] == '<' {
		at = scriptTypeArguments(text, at, ends)
		if at < 0 {
			return -1, false
		}
		at = scriptSpace(text, at)
	}
	close, ok := ends[at]
	if !ok || text[at] != '(' {
		return -1, false
	}
	for at = close + 1; at < len(text); at++ {
		if strings.ContainsRune("{;=", rune(text[at])) {
			return -1, false
		}
		if close, ok := ends[at]; ok {
			at = close
			continue
		}
		if text[at] == ':' && text[at-1] != ':' && (at+1 == len(text) || text[at+1] != ':') {
			return cppInitializerEnd(text, at+1, ends), true
		}
	}
	return -1, false
}

func cppInitializerEnd(text string, at int, ends map[int]int) int {
	for at < len(text) {
		for at < len(text) && text[at] != '(' && text[at] != '{' && text[at] != ';' {
			at++
		}
		close, ok := ends[at]
		if !ok {
			return -1
		}
		at = scriptSpace(text, close+1)
		if at < len(text) && text[at] == '{' {
			return at
		}
		if at >= len(text) || text[at] != ',' {
			return -1
		}
		at++
	}
	return -1
}
