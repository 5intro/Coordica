package codequality

import (
	"context"
	"path"
	"regexp"
	"strings"
)

var nativeClass = regexp.MustCompile(`\b(?:record(?:\s+(?:class|struct))?|class|struct|interface|trait|object|impl|enum)\b\s*` +
	`(?:<[^{};]*>\s*)?([A-Za-z_$][\w$]*)`)
var nativeFunction = regexp.MustCompile(`\b(?:fn|func|fun|function)\b\s*&?\s*([A-Za-z_$][\w$]*)?`)

type nativeBlock struct {
	name             string
	start, body, end int
}

func nativeFunctionBody(text string, at int, ends map[int]int) int {
	close := nativeParameterClose(text, at, ends)
	if close < 0 {
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

func nativeFunctions(ctx context.Context, original, text, extension string, ends map[int]int) []nativeBlock {
	var blocks []nativeBlock
	seen := map[int]bool{}
	headerEnd := 0
	matches := nativeFunction.FindAllStringSubmatchIndex(text, -1)
	keyword := extension == ".rs" || extension == ".swift" || extension == ".kt" || extension == ".php"
	if !keyword {
		matches = nativeSignatureMatches(text, extension)
	} else if extension == ".kt" {
		matches = kotlinFunction.FindAllStringSubmatchIndex(text, -1)
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
		if nativeAttributeCall(text, extension, match[0], ends) ||
			extension != ".kt" && !nativeDeclarationPrefix(text, match[0], ends) {
			continue
		}
		if extension == ".kt" {
			if fn, ok := kotlinFunctionBlock(original, text, name, match[0], at, ends); ok {
				blocks = append(blocks, fn)
				headerEnd = max(fn.body, fn.start)
			}
			continue
		}
		if extension == ".cs" {
			if body, end := csharpExpressionBody(text, at, ends); body >= 0 {
				blocks = append(blocks, nativeBlock{name, match[0], body, end})
				headerEnd = body
				continue
			}
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
		} else if body < 0 {
			if end := nativeDeclarationEnd(text, at, ends); end >= 0 {
				blocks = append(blocks, nativeBlock{name, match[0], -1, end})
				headerEnd = end
			}
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
	functions := nativeFunctions(m.ctx, f.Text, text, extension, pairs)
	if extension == ".rs" {
		recordRustFile(m, f, text, pairs)
	}
	for _, fn := range functions {
		if m.cancelled() {
			return
		}
		if fn.body >= 0 {
			m.scriptFunctionFinding(f, text, fn.name, fn.start, fn.body, fn.end, pairs)
		}
	}
	nativeAnonymousFunctions(m, f, text, extension, pairs, functions)
	counts := nativeMethodCounts(m.ctx, text, functions)
	if extension == ".java" {
		javaAnonymousClasses(m, f, text, pairs, counts)
	}
	for _, match := range nativeClass.FindAllStringSubmatchIndex(text, -1) {
		if m.cancelled() {
			return
		}
		open := scriptClassBody(text, match[1], pairs)
		close, ok := ends[open]
		if !ok {
			continue
		}
		name, methods := text[match[2]:match[3]], counts[open]
		if extension == ".rs" {
			recordRustType(m, f, text, match, open, close, methods, pairs)
			continue
		}
		if extension == ".cs" && recordCSharpPartial(m, f, text, match, close, methods, pairs) {
			continue
		}
		scopedClassFinding(m, f, text, name, "class_lines", match[0], open, m.lineAt(close)-m.lineAt(match[0])+1, MaxClass, pairs)
		scopedClassFinding(m, f, text, name, "class_methods", open, open, methods, MaxMethods, pairs)
	}
}
