package codequality

import (
	"path"
	"regexp"
	"strings"
)

var classBlock = regexp.MustCompile(`\bclass\s+([A-Za-z_$][\w$]*)[^;{}]*\{`)
var namedFunction = regexp.MustCompile(`\bfunction\s*([A-Za-z_$][\w$]*)?\s*\([^{};]*\)[^{};]*\{`)
var arrowFunction = regexp.MustCompile(`=>\s*\{`)
var classMethod = regexp.MustCompile(`([A-Za-z_$][\w$]*)\s*\([^;{}]*\)[^;{}=]*\{`)
var pythonDeclaration = regexp.MustCompile(`^(?:async\s+)?(class|def)\s+([A-Za-z_]\w*)`)

func (m *metrics) structuredSource(f File) {
	if strings.EqualFold(path.Ext(f.Path), ".py") {
		m.pythonSource(f)
		return
	}
	switch strings.ToLower(path.Ext(f.Path)) {
	case ".js", ".mjs", ".cjs", ".jsx", ".ts", ".tsx", ".java", ".cs", ".kt", ".swift":
		m.braceSource(f)
	}
}

// Token masking preserves source positions. These non-Go measurements are
// lexical checks, not language type-checking; native linters and independent
// cohesion review remain required (especially overloads and metaprogramming).
func maskedSource(text string, python bool, stringEnds map[int]int) string {
	out := []byte(text)
	blank := func(start, end int) {
		for i := start; i < end; i++ {
			if out[i] != '\n' {
				out[i] = ' '
			}
		}
	}
	for i := 0; i < len(text); {
		start := i
		switch {
		case python && text[i] == '#' || !python && strings.HasPrefix(text[i:], "//"):
			for i < len(text) && text[i] != '\n' {
				i++
			}
		case !python && strings.HasPrefix(text[i:], "/*"):
			i += 2
			for i < len(text) && !strings.HasPrefix(text[i:], "*/") {
				i++
			}
			i = min(len(text), i+2)
		case text[i] == '\'' || text[i] == '"' || text[i] == '`':
			quote := text[i : i+1]
			if python && strings.HasPrefix(text[i:], strings.Repeat(quote, 3)) {
				quote = strings.Repeat(quote, 3)
			}
			i += len(quote)
			for i < len(text) && !strings.HasPrefix(text[i:], quote) {
				if text[i] == '\\' {
					i++
				}
				i = min(len(text), i+1)
			}
			i = min(len(text), i+len(quote))
		default:
			i++
			continue
		}
		blank(start, i)
		if python && (text[start] == '\'' || text[start] == '"') {
			// A standalone string is a statement, including at module scope.
			// Preserve its opening position and complete multiline extent.
			out[start] = '0'
			stringEnds[lineAt(text, start)] = lineAt(text, i-1)
		}
	}
	return string(out)
}

func braceEnds(text string) map[int]int {
	ends := map[int]int{}
	var stack []int
	for i := 0; i < len(text); i++ {
		if text[i] == '{' {
			stack = append(stack, i)
		} else if text[i] == '}' && len(stack) > 0 {
			start := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			ends[start] = i
		}
	}
	return ends
}

func lineAt(text string, offset int) int { return strings.Count(text[:offset], "\n") + 1 }

func (m *metrics) braceSource(f File) {
	text := maskedSource(f.Text, false, nil)
	switch strings.ToLower(path.Ext(f.Path)) {
	case ".js", ".mjs", ".cjs", ".jsx", ".ts", ".tsx":
		text = maskedJavaScript(f.Text)
	}
	ends := braceEnds(text)
	for _, match := range classBlock.FindAllStringSubmatchIndex(text, -1) {
		open := match[1] - 1
		end, ok := ends[open]
		if !ok {
			continue // Native syntax validation must reject incomplete programs.
		}
		name := text[match[2]:match[3]]
		m.add(f.Path, lineAt(text, match[0]), "class_lines", name,
			lineAt(text, end)-lineAt(text, match[0])+1, MaxClass)
		m.classMembers(f.Path, text, name, open, end, ends)
	}
	for _, match := range namedFunction.FindAllStringSubmatchIndex(text, -1) {
		name := "anonymous"
		if match[2] >= 0 {
			name = text[match[2]:match[3]]
		}
		m.blockFunction(f.Path, text, name, match[0], match[1]-1, ends)
	}
	for _, match := range arrowFunction.FindAllStringIndex(text, -1) {
		m.blockFunction(f.Path, text, "arrow", match[0], match[1]-1, ends)
	}
}

func (m *metrics) blockFunction(file, text, name string, start, open int, ends map[int]int) {
	if end, ok := ends[open]; ok {
		m.add(file, lineAt(text, start), "function_lines", name,
			lineAt(text, end)-lineAt(text, start)+1, MaxFunction)
	}
}

func (m *metrics) classMembers(file, text, class string, open, end int, ends map[int]int) {
	count, cursor := 0, open+1
	walked, depth := open+1, 0
	for _, match := range classMethod.FindAllStringSubmatchIndex(text[open+1:end], -1) {
		start, body := open+1+match[0], open+match[1]
		if start < cursor {
			continue
		}
		for walked < start {
			if text[walked] == '{' {
				depth++
			} else if text[walked] == '}' {
				depth--
			}
			walked++
		}
		if depth != 0 {
			continue
		}
		name := text[open+1+match[2] : open+1+match[3]]
		if close, ok := ends[body]; ok {
			count++
			m.blockFunction(file, text, class+"."+name, start, body, ends)
			cursor = close + 1
		}
	}
	m.add(file, lineAt(text, open), "class_methods", class, count, MaxMethods)
}

func (m *metrics) pythonSource(f File) {
	stringEnds := map[int]int{}
	lines := strings.Split(maskedSource(f.Text, true, stringEnds), "\n")
	original := strings.Split(f.Text, "\n")
	for start, line := range lines {
		trimmed := strings.TrimSpace(line)
		decl := pythonDeclaration.FindStringSubmatch(trimmed)
		if decl == nil {
			continue
		}
		end, methods := pythonRegion(lines, original, stringEnds, start)
		if decl[1] == "class" {
			m.add(f.Path, start+1, "class_lines", decl[2], end-start+1, MaxClass)
			m.add(f.Path, start+1, "class_methods", decl[2], methods, MaxMethods)
		} else {
			m.add(f.Path, start+1, "function_lines", decl[2], end-start+1, MaxFunction)
		}
	}
}

func pythonRegion(lines, original []string, stringEnds map[int]int, start int) (int, int) {
	indent := indentation(lines[start])
	headerEnd, depth := start, 0
header:
	for i := start; i < len(lines); i++ {
		for _, c := range lines[i] {
			switch c {
			case '(', '[', '{':
				depth++
			case ')', ']', '}':
				depth--
			case ':':
				if depth == 0 {
					headerEnd = i
					break header
				}
			}
		}
	}
	end, bodyIndent := headerEnd, -1
	for i := headerEnd + 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != "" {
			d := indentation(lines[i])
			if d <= indent {
				break
			}
			if bodyIndent == -1 || d < bodyIndent {
				bodyIndent = d
			}
			end = max(end, i, stringEnds[i+1]-1)
		}
		if strings.TrimSpace(original[i]) != "" && indentation(original[i]) > indent {
			end = max(end, i)
		}
	}
	methods := 0
	for _, line := range lines[headerEnd+1 : end+1] {
		decl := pythonDeclaration.FindStringSubmatch(strings.TrimSpace(line))
		if decl != nil && decl[1] == "def" && indentation(line) == bodyIndent {
			methods++
		}
	}
	return end, methods
}

func indentation(line string) int {
	return len(line) - len(strings.TrimLeft(line, " \t"))
}
