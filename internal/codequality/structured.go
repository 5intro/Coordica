package codequality

import (
	"path"
	"regexp"
	"strings"
)

var classBlock = regexp.MustCompile(`\bclass(?:\s+([A-Za-z_$][\w$]*))?`)
var pythonDeclaration = regexp.MustCompile(`^(?:async\s+)?(class|def)\s+([A-Za-z_]\w*)`)

func (m *metrics) structuredSource(f File) {
	if strings.EqualFold(path.Ext(f.Path), ".py") {
		m.pythonSource(f)
		return
	}
	switch strings.ToLower(path.Ext(f.Path)) {
	case ".js", ".mjs", ".cjs", ".jsx", ".ts", ".tsx":
		m.javaScriptSource(f)
	case ".java", ".cs", ".kt", ".swift", ".rs", ".c", ".cc", ".cpp", ".h", ".hpp", ".php":
		m.nativeBraceSource(f)
	case ".rb":
		m.rubySource(f)
	case ".sh", ".ps1", ".scss":
		m.commandSource(f)
	case ".html", ".vue", ".svelte":
		m.embeddedSource(f)
	}
}

// Token masking preserves source positions. These non-Go measurements are
// lexical checks, not language type-checking; native linters and independent
// cohesion review remain required (especially overloads and metaprogramming).
func maskedSource(text string, python bool, stringEnds map[int]int) string {
	out := []byte(text)
	lineOffsets := sourceLineOffsets(text)
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
			stringEnds[indexedLine(lineOffsets, start)] = indexedLine(lineOffsets, i-1)
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

func (m *metrics) blockFunction(file, text, name string, start, open int, ends map[int]int) {
	if end, ok := ends[open]; ok {
		m.add(file, m.lineAt(start), "function_lines", name,
			m.lineAt(end)-m.lineAt(start)+1, MaxFunction)
	}
}

func (m *metrics) pythonSource(f File) {
	stringEnds := map[int]int{}
	lines := strings.Split(maskedSource(f.Text, true, stringEnds), "\n")
	original := strings.Split(f.Text, "\n")
	pythonLambdas(m, f)
	type scope struct {
		name string
		end  int
	}
	var scopes []scope
	for start, line := range lines {
		if m.cancelled() {
			return
		}
		trimmed := strings.TrimSpace(line)
		decl := pythonDeclaration.FindStringSubmatch(trimmed)
		if decl == nil {
			continue
		}
		end, methods := pythonRegion(lines, original, stringEnds, start)
		for len(scopes) > 0 && start > scopes[len(scopes)-1].end {
			scopes = scopes[:len(scopes)-1]
		}
		name := decl[2]
		if len(scopes) > 0 {
			name = scopes[len(scopes)-1].name + "." + name
		}
		if decl[1] == "class" {
			m.add(f.Path, start+1, "class_lines", name, end-start+1, MaxClass)
			m.add(f.Path, start+1, "class_methods", name, methods, MaxMethods)
		} else {
			m.add(f.Path, start+1, "function_lines", name, end-start+1, MaxFunction)
		}
		scopes = append(scopes, scope{name, end})
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
