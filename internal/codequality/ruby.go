package codequality

import (
	"regexp"
	"strings"
)

var rubyToken = regexp.MustCompile(`\b(?:class|module|def|if|unless|case|begin|while|until|for|do|end)\b`)
var rubyName = regexp.MustCompile(`^(?:self\.)?[A-Za-z_]\w*[!?=]?`)

type rubyBlock struct {
	kind, name    string
	line, methods int
}

func (m *metrics) rubySource(f File) {
	literals := map[int]int{}
	text := maskedRubyLiterals(f.Text, literals)
	pairs := scriptPairs(text)
	for start, end := range literals {
		pairs[start] = end
	}
	rubyBraceBlocks(m, f, text, pairs)
	var stack []rubyBlock
	skipUntil := -1
	for line, source := range strings.Split(text, "\n") {
		if m.cancelled() {
			return
		}
		loop := false
		for _, token := range rubyToken.FindAllStringIndex(source, -1) {
			absolute := m.lines[line] + token[0]
			if absolute <= skipUntil || rubyQualifiedKeyword(source, token[0]) {
				continue
			}
			word := source[token[0]:token[1]]
			prefix := strings.TrimSpace(source[:token[0]])
			if word == "end" {
				if len(stack) > 0 {
					frame := stack[len(stack)-1]
					stack = stack[:len(stack)-1]
					m.rubyBlockFinding(f.Path, frame, line+1)
				}
				continue
			}
			if (word == "if" || word == "unless" || word == "while" || word == "until") && prefix != "" && !strings.HasSuffix(prefix, ";") {
				continue // Statement modifiers have no matching end.
			}
			if word == "do" && loop {
				continue
			}
			loop = word == "while" || word == "until" || word == "for"
			name := rubyName.FindString(strings.TrimSpace(source[token[1]:]))
			if word == "def" {
				rubyCountMethod(stack)
				if end, ok := rubyEndlessEnd(m, f, text, m.lines[line]+token[1], pairs); ok {
					m.add(f.Path, line+1, "function_lines", rubyScopedName(stack, name), m.lineAt(end)-line, MaxFunction)
					skipUntil = end
					continue
				}
			}
			stack = append(stack, rubyBlock{kind: word, name: rubyScopedName(stack, name), line: line + 1})
		}
	}
}

func rubyCountMethod(stack []rubyBlock) {
	for i := len(stack) - 1; i >= 0; i-- {
		if stack[i].kind == "def" || stack[i].kind == "do" {
			return
		}
		if stack[i].kind == "class" {
			stack[i].methods++
			return
		}
	}
}

func rubyScopedName(stack []rubyBlock, name string) string {
	for i := len(stack) - 1; i >= 0; i-- {
		if stack[i].kind == "class" || stack[i].kind == "module" || stack[i].kind == "def" {
			return stack[i].name + "." + name
		}
	}
	return name
}

func (m *metrics) rubyBlockFinding(file string, block rubyBlock, end int) {
	switch block.kind {
	case "def", "do":
		m.add(file, block.line, "function_lines", block.name, end-block.line+1, MaxFunction)
	case "class":
		m.add(file, block.line, "class_lines", block.name, end-block.line+1, MaxClass)
		m.add(file, block.line, "class_methods", block.name, block.methods, MaxMethods)
	}
}

// Receiver method names and unquoted symbols do not open keyword blocks.
func rubyQualifiedKeyword(source string, at int) bool {
	prefix := strings.TrimRight(source[:at], " \t")
	return strings.HasSuffix(prefix, ".") || strings.HasSuffix(prefix, ":")
}
