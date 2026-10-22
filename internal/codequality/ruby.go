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
	text := maskedSource(f.Text, true, map[int]int{})
	var stack []rubyBlock
	for line, source := range strings.Split(text, "\n") {
		loop := false
		for _, token := range rubyToken.FindAllStringIndex(source, -1) {
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
				for i := len(stack) - 1; i >= 0; i-- {
					if stack[i].kind == "def" || stack[i].kind == "do" {
						break
					}
					if stack[i].kind == "class" {
						stack[i].methods++
						break
					}
				}
			}
			stack = append(stack, rubyBlock{kind: word, name: name, line: line + 1})
		}
	}
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
