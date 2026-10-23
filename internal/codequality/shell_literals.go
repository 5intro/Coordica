package codequality

import "strings"

type shellLiteralFrame struct {
	quote            byte
	depth, parameter int
	escape           bool
}

// Quoted text is inert, while $(...) and backticks retain executable code.
// An explicit stack preserves nested substitutions without recursive parsing.
func shellExecutableText(text string) string {
	out := []byte(text)
	stack := []shellLiteralFrame{{}}
	var pending []shellDocument
	for at := 0; at < len(text); at++ {
		frame := &stack[len(stack)-1]
		if frame.quote == '\'' {
			if frame.escape && text[at] == '\\' {
				scriptBlank(out, at, min(len(text), at+2))
				at++
				continue
			}
			scriptBlank(out, at, at+1)
			if text[at] == '\'' {
				stack = stack[:len(stack)-1]
			}
			continue
		}
		if text[at] == '\\' {
			scriptBlank(out, at, min(len(text), at+2))
			at++
			continue
		}
		if frame.quote == '"' {
			at, stack = shellQuotedExpansion(text, out, at, stack)
			continue
		}
		if strings.HasPrefix(text[at:], "((") {
			at = shellArithmeticEnd(text, at) - 1
			continue
		}
		if frame.quote == ')' && (text[at] == '(' || text[at] == ')') {
			if text[at] == '(' {
				frame.depth++
			} else {
				frame.depth--
				if frame.depth == 0 {
					stack = stack[:len(stack)-1]
				}
			}
			continue
		}
		if frame.quote == '`' && text[at] == '`' {
			scriptBlank(out, at, at+1)
			stack = stack[:len(stack)-1]
			continue
		}
		if text[at] == '\'' || text[at] == '"' || text[at] == '`' {
			scriptBlank(out, at, at+1)
			stack = append(stack, shellLiteralFrame{quote: text[at], escape: at > 0 && text[at-1] == '$'})
		} else {
			at, pending = shellDocumentAt(text, out, at, pending)
			at = shellCodeLiteral(text, out, at, frame)
		}
	}
	return string(out)
}

func shellQuotedExpansion(text string, out []byte, at int, stack []shellLiteralFrame) (int, []shellLiteralFrame) {
	if strings.HasPrefix(text[at:], "$(") {
		return at + 1, append(stack, shellLiteralFrame{quote: ')', depth: 1})
	}
	scriptBlank(out, at, at+1)
	if text[at] == '`' {
		stack = append(stack, shellLiteralFrame{quote: '`'})
	} else if text[at] == '"' {
		stack = stack[:len(stack)-1]
	}
	return at, stack
}

func shellCodeLiteral(text string, out []byte, at int, frame *shellLiteralFrame) int {
	if strings.HasPrefix(text[at:], "${") {
		frame.parameter++
		return at + 1
	}
	if text[at] == '}' && frame.parameter > 0 {
		frame.parameter--
	}
	if text[at] != '#' {
		return at
	}
	if frame.parameter > 0 || at > 0 && !strings.ContainsRune(" \t\r\n;|&()", rune(text[at-1])) {
		out[at] = '+'
		return at
	}
	end := at
	for end < len(text) && text[end] != '\n' {
		end++
	}
	scriptBlank(out, at, end)
	return end - 1
}
