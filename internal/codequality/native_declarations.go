package codequality

import "strings"

func nativeDeclarationPrefix(text string, start int, ends map[int]int) bool {
	begin := strings.LastIndexAny(text[:start], ";{}") + 1
	for at := begin; at < start; at++ {
		if close, ok := ends[at]; ok && close < start {
			at = close
		} else if text[at] == '=' {
			return false
		}
	}
	words := strings.Fields(text[begin:start])
	if len(words) > 0 {
		switch words[len(words)-1] {
		case "class", "struct", "interface", "trait", "record", "new":
			return false
		}
	}
	return true
}

func nativeDeclarationEnd(text string, at int, ends map[int]int) int {
	close := nativeParameterClose(text, at, ends)
	if close < 0 {
		return -1
	}
	for at = close + 1; at < len(text); at++ {
		if text[at] == ';' {
			return at
		}
		if text[at] == '{' || text[at] == '}' {
			return -1
		}
		if close, ok := ends[at]; ok {
			at = close
		}
	}
	return -1
}
