package codequality

import (
	"regexp"
	"strings"
)

var csharpDelegateExpression = regexp.MustCompile(`\bdelegate\s*([({])`)

func nativeArrowLambdas(m *metrics, f File, text, extension string, ends map[int]int, functions []nativeBlock) {
	starts, methodArrows := map[int]int{}, map[int]bool{}
	for open, close := range ends {
		starts[close] = open
	}
	for _, fn := range functions {
		if fn.body >= 0 {
			methodArrows[fn.body] = true
		}
	}
	for at := 0; at < len(text)-1; at++ {
		if m.cancelled() {
			return
		}
		arrow := extension == ".cs" && strings.HasPrefix(text[at:], "=>") || extension == ".java" && strings.HasPrefix(text[at:], "->")
		if !arrow || methodArrows[at] {
			continue
		}
		start := scriptArrowStart(text, at, starts)
		if extension == ".java" && javaSwitchArm(text, start) {
			continue
		}
		body := scriptSpace(f.Text, at+2)
		if body >= len(text) {
			continue
		}
		end := -1
		if text[body] == '{' {
			end = ends[body]
		} else {
			end = nativeExpressionEnd(f.Text, text, body, ends)
		}
		if end >= body {
			m.scriptFunctionFinding(f, text, "lambda", start, body, end, ends)
		}
	}
	if extension == ".cs" {
		for _, match := range csharpDelegateExpression.FindAllStringSubmatchIndex(text, -1) {
			body := match[2]
			if text[body] == '(' {
				if close, ok := ends[body]; ok {
					body = scriptSpace(text, close+1)
				}
			}
			if end, ok := ends[body]; ok && text[body] == '{' {
				m.scriptFunctionFinding(f, text, "delegate", match[0], body, end, ends)
			}
		}
	}
}

func nativeExpressionEnd(original, text string, start int, pairs map[int]int) int {
	end := len(text)
	for at := start; at < len(text); at++ {
		if strings.ContainsRune(";,)]}", rune(text[at])) {
			end = at
			break
		}
		if close, ok := pairs[at]; ok {
			at = close
		}
	}
	return start + len(strings.TrimRight(original[start:end], " \t\r\n")) - 1
}

func javaSwitchArm(text string, start int) bool {
	begin := strings.LastIndexAny(text[:start], ";{}") + 1
	for _, word := range strings.Fields(text[begin:start]) {
		if word == "case" || word == "default" {
			return true
		}
	}
	return false
}
