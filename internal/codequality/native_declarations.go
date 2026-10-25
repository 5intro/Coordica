package codequality

import "strings"

func nativeAttributeCall(text, extension string, start int, ends map[int]int) bool {
	if extension == ".java" {
		at := start - 1
		for at >= 0 && (scriptWord(text[at]) || text[at] == '.') {
			at--
		}
		return at >= 0 && text[at] == '@'
	}
	if extension == ".cs" {
		open := strings.LastIndexByte(text[:start], '[')
		return open >= 0 && ends[open] > start
	}
	return false
}

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
	for _, word := range words {
		if word == "delegate" {
			return false
		}
	}
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
