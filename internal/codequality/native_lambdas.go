package codequality

import "strings"

func nativeAnonymousFunctions(m *metrics, f File, text, extension string, ends map[int]int) {
	if extension == ".rs" {
		rustClosures(m, f, text, ends)
	}
	if strings.Contains("|.cc|.cpp|.h|.hpp|", "|"+extension+"|") {
		for open, close := range ends {
			if m.cancelled() {
				return
			}
			if text[open] != '[' || !cppCapturePrefix(text, open) || open+1 < len(text) && text[open+1] == '[' {
				continue
			}
			body := cppLambdaBody(text, close+1, ends)
			if end, ok := ends[body]; ok && body >= 0 {
				m.scriptFunctionFinding(f, text, "lambda", open, body, end, ends)
			}
		}
	}
	if extension == ".cs" || extension == ".java" {
		starts := map[int]int{}
		for open, close := range ends {
			starts[close] = open
		}
		for at := 0; at < len(text)-1; at++ {
			if m.cancelled() {
				return
			}
			arrow := extension == ".cs" && strings.HasPrefix(text[at:], "=>") ||
				extension == ".java" && strings.HasPrefix(text[at:], "->")
			if body := scriptSpace(text, at+2); arrow && body < len(text) && text[body] == '{' {
				if end, ok := ends[body]; ok {
					m.scriptFunctionFinding(f, text, "lambda", scriptArrowStart(text, at, starts), body, end, ends)
				}
			}
		}
	}
}

func cppCapturePrefix(text string, at int) bool {
	for at--; at >= 0 && strings.ContainsRune(" \t\r\n", rune(text[at])); at-- {
	}
	if at < 0 || strings.ContainsRune("=(:,{;?", rune(text[at])) {
		return true
	}
	end := at + 1
	for at >= 0 && scriptWord(text[at]) {
		at--
	}
	return text[at+1:end] == "return" || text[at+1:end] == "co_return"
}

func cppLambdaBody(text string, at int, ends map[int]int) int {
	at = scriptSpace(text, at)
	if at < len(text) && text[at] == '<' {
		at = scriptTypeArguments(text, at, ends)
		if at < 0 {
			return -1
		}
		at = scriptSpace(text, at)
	}
	if at < len(text) && text[at] == '(' {
		return nativeFunctionBody(text, at, ends)
	}
	for at < len(text) {
		if text[at] == '{' {
			return at
		}
		if close, ok := ends[at]; ok && text[at] == '(' {
			at = scriptSpace(text, close+1)
			continue
		}
		start := at
		for at < len(text) && scriptWord(text[at]) {
			at++
		}
		switch text[start:at] {
		case "mutable", "constexpr", "consteval", "noexcept", "static":
			at = scriptSpace(text, at)
		default:
			return -1
		}
	}
	return -1
}
