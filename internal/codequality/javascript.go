package codequality

import "strings"

// JavaScript gives '/' two lexical meanings. Keep expression position and
// control-condition parentheses so regex braces never become block braces,
// while ordinary division remains visible. Native syntax checks still apply.
func maskedJavaScript(text string) string {
	out := []byte(text)
	canRegex, previous := true, ""
	var controls []bool
	var interpolations []int
	template := false
	for i := 0; i < len(text); {
		if template {
			var expression bool
			i, expression = maskScriptTemplate(text, out, i)
			template, canRegex, previous = false, expression, "literal"
			if expression {
				interpolations = append(interpolations, 0)
			}
			continue
		}
		start, c := i, text[i]
		switch {
		case strings.ContainsRune(" \r\n\t", rune(c)):
			i++
		case strings.HasPrefix(text[i:], "//"):
			for i < len(text) && text[i] != '\n' {
				i++
			}
			scriptBlank(out, start, i)
		case strings.HasPrefix(text[i:], "/*"):
			i += 2
			for i < len(text) && !strings.HasPrefix(text[i:], "*/") {
				i++
			}
			i = min(len(text), i+2)
			scriptBlank(out, start, i)
		case c == '`':
			out[i], i, template = '0', i+1, true
		case c == '\'' || c == '"':
			i = scriptQuoteEnd(text, i)
			scriptBlank(out, start, i)
			out[start] = '0'
			canRegex, previous = false, "literal"
		case c == '/' && canRegex && scriptRegexEnd(text, i) > i:
			i = scriptRegexEnd(text, i)
			scriptBlank(out, start, i)
			out[start] = '0'
			canRegex, previous = false, "literal"
		case scriptWord(c):
			for i < len(text) && scriptWord(text[i]) {
				i++
			}
			previous = text[start:i]
			canRegex = scriptPrefix(previous)
		default:
			if len(interpolations) > 0 {
				top := len(interpolations) - 1
				if c == '}' && interpolations[top] == 0 {
					interpolations, template, i = interpolations[:top], true, i+1
					continue
				}
				if c == '{' {
					interpolations[top]++
				} else if c == '}' {
					interpolations[top]--
				}
			}
			canRegex, controls = scriptPunctuation(c, previous, canRegex, controls)
			previous = string(c)
			i++
		}
	}
	return string(out)
}

func scriptQuoteEnd(text string, start int) int {
	quote := text[start]
	for i := start + 1; i < len(text); i++ {
		if text[i] == '\\' {
			i++
		} else if text[i] == quote {
			return i + 1
		}
	}
	return len(text)
}

func scriptRegexEnd(text string, start int) int {
	characterClass := false
	for i := start + 1; i < len(text); i++ {
		switch text[i] {
		case '\n', '\r':
			return start
		case '\\':
			i++
		case '[':
			characterClass = true
		case ']':
			characterClass = false
		case '/':
			if !characterClass {
				i++
				for i < len(text) && scriptWord(text[i]) {
					i++
				}
				return i
			}
		}
	}
	return start
}

func scriptWord(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '$' || c >= 128
}

func scriptPrefix(word string) bool {
	switch word {
	case "return", "throw", "case", "delete", "void", "typeof", "new", "yield", "await", "in", "instanceof", "of", "else", "do":
		return true
	}
	return false
}

func scriptPunctuation(c byte, previous string, allowed bool, controls []bool) (bool, []bool) {
	switch c {
	case '(':
		control := previous == "if" || previous == "while" || previous == "for" || previous == "with" || previous == "switch" || previous == "catch"
		return true, append(controls, control)
	case ')':
		if len(controls) > 0 {
			return controls[len(controls)-1], controls[:len(controls)-1]
		}
		return false, controls
	case ']', '.':
		return false, controls
	case '+', '-':
		if previous == string(c) {
			return allowed, controls
		}
	}
	return true, controls
}
