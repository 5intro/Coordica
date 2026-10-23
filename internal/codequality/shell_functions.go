package codequality

import (
	"context"
	"strings"
)

func shellFunctions(m *metrics, f File, text string, ends map[int]int) {
	for _, match := range shellFunction.FindAllStringSubmatchIndex(text, -1) {
		if m.cancelled() {
			return
		}
		start, nameEnd := match[2], match[3]
		if start < 0 {
			start, nameEnd = match[4], match[5]
		}
		body := scriptSpace(text, match[1])
		if end := shellCompoundEnd(m.ctx, text, body, ends); end >= body {
			m.scriptFunctionFinding(f, text, text[start:nameEnd], start, body, end, ends)
		}
	}
}

func shellCompoundEnd(ctx context.Context, text string, at int, ends map[int]int) int {
	if end, ok := ends[at]; ok {
		return end
	}
	var stack []string
	command := true
	for at < len(text) {
		if ctx != nil && ctx.Err() != nil {
			return -1
		}
		if end, ok := ends[at]; ok {
			if len(stack) == 0 {
				return end
			}
			at, command = end+1, false
			continue
		}
		if strings.ContainsRune("\n;|&", rune(text[at])) {
			command = true
			at++
			continue
		}
		if strings.ContainsRune(" \t\r!", rune(text[at])) {
			at++
			continue
		}
		start := at
		for at < len(text) && (scriptWord(text[at]) || text[at] == '-') {
			at++
		}
		if start == at {
			command = text[at] == ')'
			at++
			continue
		}
		word := text[start:at]
		if command {
			switch word {
			case "if":
				stack = append(stack, "fi")
			case "for", "while", "until", "select":
				stack = append(stack, "done")
			case "case":
				stack = append(stack, "esac")
			case "fi", "done", "esac":
				if len(stack) == 0 || stack[len(stack)-1] != word {
					return -1
				}
				stack = stack[:len(stack)-1]
				if len(stack) == 0 {
					return at - 1
				}
			}
		}
		command = command && (word == "if" || word == "while" || word == "until" ||
			word == "then" || word == "do" || word == "else" || word == "elif" || word == "time" || word == "coproc")
	}
	return -1
}
