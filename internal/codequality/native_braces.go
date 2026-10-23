package codequality

import (
	"context"
	"path"
	"regexp"
	"strings"
)

var nativeClass = regexp.MustCompile(`\b(?:class|struct|interface|trait|object|impl)\s+(?:<[^{};]*>\s*)?([A-Za-z_$][\w$]*)[^;{}]*\{`)
var nativeFunction = regexp.MustCompile(`\b(?:fn|func|fun|function)\b\s*&?\s*([A-Za-z_$][\w$]*)?`)

type nativeBlock struct {
	name             string
	start, body, end int
}

func nativeFunctionBody(text string, at int, ends map[int]int) int {
	at = scriptSpace(text, at)
	if at < len(text) && text[at] == '<' {
		at = scriptTypeArguments(text, at, ends)
		if at < 0 {
			return -1
		}
		at = scriptSpace(text, at)
	}
	if at >= len(text) || text[at] != '(' {
		return -1
	}
	close, ok := ends[at]
	if !ok {
		return -1
	}
	for at = close + 1; at < len(text); at++ {
		if text[at] == '{' {
			return at
		}
		if text[at] == ';' || text[at] == '=' && !strings.HasPrefix(text[at:], "=>") {
			return -1
		}
		if close, ok := ends[at]; ok {
			at = close
		}
	}
	return -1
}

func nativeFunctions(ctx context.Context, text, extension string, ends map[int]int) []nativeBlock {
	var blocks []nativeBlock
	seen := map[int]bool{}
	headerEnd := 0
	matches := nativeFunction.FindAllStringSubmatchIndex(text, -1)
	keyword := extension == ".rs" || extension == ".swift" || extension == ".kt" || extension == ".php"
	if !keyword {
		matches = scriptSignature.FindAllStringSubmatchIndex(text, -1)
	}
	for _, match := range matches {
		if ctx != nil && ctx.Err() != nil {
			return nil
		}
		if match[0] < headerEnd {
			continue
		}
		name, at := "anonymous", match[1]
		if match[2] >= 0 {
			name, at = text[match[2]:match[3]], match[3]
		}
		if !keyword && nativeControl(name) {
			continue
		}
		body := nativeFunctionBody(text, at, ends)
		if strings.Contains("|.cc|.cpp|.h|.hpp|", "|"+extension+"|") {
			if constructor, ok := cppInitializerBody(text, at, ends); ok {
				body = constructor
			}
		}
		if close, ok := ends[body]; ok && body >= 0 && !seen[body] {
			blocks = append(blocks, nativeBlock{name, match[0], body, close})
			seen[body] = true
			headerEnd = body
		}
	}
	return blocks
}

func nativeControl(name string) bool {
	switch name {
	case "if", "for", "while", "switch", "catch", "when", "using", "lock", "synchronized", "sizeof", "alignof":
		return true
	}
	return false
}

func enclosingBrace(ends map[int]int, start int) int {
	parent := -1
	for open, close := range ends {
		if open < start && close > start && open > parent {
			parent = open
		}
	}
	return parent
}

func (m *metrics) nativeBraceSource(f File) {
	extension := strings.ToLower(path.Ext(f.Path))
	text := nativeMaskedSource(f.Text, extension)
	ends := braceEnds(text)
	pairs := scriptPairs(text)
	functions := nativeFunctions(m.ctx, text, extension, pairs)
	for _, fn := range functions {
		if m.cancelled() {
			return
		}
		m.blockFunction(f.Path, text, fn.name, fn.start, fn.body, ends)
	}
	nativeAnonymousFunctions(m, f, text, extension, pairs)
	for _, match := range nativeClass.FindAllStringSubmatchIndex(text, -1) {
		if m.cancelled() {
			return
		}
		open := match[1] - 1
		close, ok := ends[open]
		if !ok {
			continue
		}
		name, methods := text[match[2]:match[3]], 0
		for _, fn := range functions {
			if m.cancelled() {
				return
			}
			if enclosingBrace(ends, fn.start) == open {
				methods++
			}
		}
		m.add(f.Path, m.lineAt(match[0]), "class_lines", name, m.lineAt(close)-m.lineAt(match[0])+1, MaxClass)
		m.add(f.Path, m.lineAt(open), "class_methods", name, methods, MaxMethods)
	}
}
