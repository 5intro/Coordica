package codequality

import (
	"regexp"
	"strings"
)

var scriptFunction = regexp.MustCompile(`\bfunction\s*\*?\s*([A-Za-z_$][\w$]*)?`)
var scriptSignature = regexp.MustCompile(`\b([A-Za-z_$][\w$]*)\s*(?:<|\()`)

func scriptSpace(text string, at int) int {
	for at < len(text) && strings.ContainsRune(" \t\r\n", rune(text[at])) {
		at++
	}
	return at
}

// Balance parameter destructuring, defaults, type objects and nested calls.
// Positions remain offsets into the authored source, without transpilation.
func scriptPairs(text string) map[int]int {
	ends := map[int]int{}
	var stack []int
	for i := 0; i < len(text); i++ {
		if strings.ContainsRune("({[", rune(text[i])) {
			stack = append(stack, i)
		} else if len(stack) > 0 && strings.ContainsRune(")}]", rune(text[i])) {
			open := stack[len(stack)-1]
			if strings.IndexByte("({[", text[open]) == strings.IndexByte(")}]", text[i]) {
				ends[open] = i
				stack = stack[:len(stack)-1]
			}
		}
	}
	return ends
}

func scriptTypeArguments(text string, at int, ends map[int]int) int {
	depth := 0
	for i := at; i < len(text); i++ {
		switch text[i] {
		case '(', '{', '[':
			end, ok := ends[i]
			if !ok {
				return -1
			}
			i = end
		case '<':
			depth++
		case '>':
			if i > 0 && text[i-1] == '=' {
				continue // A function type's arrow is not a generic close.
			}
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return -1
}

func scriptFunctionBody(text string, at int, ends map[int]int) int {
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
	at = scriptSpace(text, close+1)
	if at < len(text) && text[at] == '{' {
		return at
	}
	if at < len(text) && text[at] == ':' {
		return scriptTypedBody(text, at+1, ends)
	}
	return -1
}

func scriptTypedBody(text string, at int, ends map[int]int) int {
	expectsType := true
	for i := at; i < len(text); {
		i = scriptSpace(text, i)
		if i == len(text) || text[i] == ';' || text[i] == '=' && !strings.HasPrefix(text[i:], "=>") {
			return -1
		}
		if text[i] == '{' && !expectsType {
			return i
		}
		if close, ok := ends[i]; ok {
			i, expectsType = close+1, false
			continue
		}
		if text[i] == '<' {
			i = scriptTypeArguments(text, i, ends)
			if i < 0 {
				return -1
			}
			continue
		}
		if scriptWord(text[i]) {
			start := i
			for i < len(text) && scriptWord(text[i]) {
				i++
			}
			word := text[start:i]
			expectsType = word == "is" || word == "extends" || word == "keyof" || word == "typeof"
			continue
		}
		if strings.HasPrefix(text[i:], "=>") {
			i, expectsType = i+2, true
		} else {
			expectsType = strings.ContainsRune("|&?:", rune(text[i]))
			i++
		}
	}
	return -1
}

func (m *metrics) javaScriptSource(f File) {
	text := maskedJavaScript(f.Text)
	ends := scriptPairs(text)
	seen := map[int]bool{}
	for _, match := range classBlock.FindAllStringSubmatchIndex(text, -1) {
		if m.cancelled() {
			return
		}
		open := scriptClassBody(text, match[1], ends)
		if end, ok := ends[open]; ok {
			name := scriptClassName(m.ctx, f.Text, text, match, open, ends)
			m.add(f.Path, m.lineAt(match[0]), "class_lines", name,
				m.lineAt(end)-m.lineAt(match[0])+1, MaxClass)
			m.scriptMembers(f, text, name, open, end, ends, seen)
		}
	}
	for _, match := range scriptFunction.FindAllStringSubmatchIndex(text, -1) {
		if m.cancelled() {
			return
		}
		name := "anonymous"
		if match[2] >= 0 {
			name = text[match[2]:match[3]]
		}
		if body := scriptFunctionBody(text, match[1], ends); body >= 0 {
			if close, ok := ends[body]; ok {
				m.scriptFunctionFinding(f, text, name, match[0], body, close, ends)
				seen[body] = true
			}
		}
	}
	m.scriptObjectMethods(f, text, ends, seen)
	m.scriptArrows(f, text, ends)
}

func (m *metrics) scriptMembers(f File, text, class string, open, end int, ends map[int]int, seen map[int]bool) {
	count, cursor, walked, depth := 0, open+1, open+1, 0
	for _, match := range scriptMethodMatches(m.ctx, text, ends) {
		if m.cancelled() {
			return
		}
		start := match[0]
		if start < cursor || start >= end {
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
		prefix := text[max(cursor, strings.LastIndex(text[:start], "\n")+1):start]
		if depth != 0 || strings.Contains(prefix, "=") || nativeAttributeCall(text, ".java", start, ends) {
			continue
		}
		nameEnd := match[3]
		body := scriptFunctionBody(text, nameEnd, ends)
		if close, ok := ends[body]; ok && body >= 0 {
			name := f.Text[match[2]:nameEnd]
			count++
			m.scriptFunctionFinding(f, text, class+"."+name, start, body, close, ends)
			seen[body] = true
			cursor = close + 1
		} else if close := scriptBodylessMemberEnd(text, nameEnd, end, ends); close >= 0 {
			count++
			cursor = close + 1
		}
	}
	count += scriptCallableFields(m.ctx, f.Text, text, open, end, ends)
	m.add(f.Path, m.lineAt(open), "class_methods", class, count, MaxMethods)
}
