package codequality

import (
	"context"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var csharpNamespace = regexp.MustCompile(`\bnamespace\s+([A-Za-z_][\w.]*)\s*([;{])`)
var partialModifier = regexp.MustCompile(`\bpartial\b`)
var fileModifier = regexp.MustCompile(`\bfile\b`)

type partialMeasurement struct {
	file, name  string
	line, lines int
	methods     int
}

func recordCSharpPartial(m *metrics, f File, text string, match []int, close, methods int, ends map[int]int) bool {
	begin := strings.LastIndexAny(text[:match[0]], ";{}") + 1
	if !partialModifier.MatchString(text[begin:match[0]]) {
		return false
	}
	name := csharpTypeIdentity(m.ctx, text, match[0], match[2], match[3], ends)
	if fileModifier.MatchString(text[begin:match[0]]) {
		name = f.Path + ":" + name
	}
	if m.partials == nil {
		m.partials = map[string]*partialMeasurement{}
	}
	for _, scope := range m.csharpScopes {
		key := scope + "\x00" + name
		value := m.partials[key]
		if value == nil {
			value = &partialMeasurement{file: f.Path, line: m.lineAt(match[0]), name: name}
			m.partials[key] = value
		}
		value.lines += m.lineAt(close) - m.lineAt(match[0]) + 1
		value.methods += methods
	}
	return true
}

func csharpTypeIdentity(ctx context.Context, text string, at, nameStart, nameEnd int, ends map[int]int) string {
	type scope struct {
		at   int
		name string
	}
	var scopes []scope
	for _, namespace := range csharpNamespace.FindAllStringSubmatchIndex(text[:at], -1) {
		open := namespace[1] - 1
		if text[open] == ';' || ends[open] > at {
			scopes = append(scopes, scope{open, text[namespace[2]:namespace[3]]})
		}
	}
	for _, declaration := range nativeClass.FindAllStringSubmatchIndex(text[:at], -1) {
		if ctx != nil && ctx.Err() != nil {
			return ""
		}
		open := scriptClassBody(text, declaration[1], ends)
		if end, ok := ends[open]; ok && end > at {
			name := csharpGenericName(text, declaration[2], declaration[3], ends)
			scopes = append(scopes, scope{open, name})
		}
	}
	sort.Slice(scopes, func(i, j int) bool { return scopes[i].at < scopes[j].at })
	parts := make([]string, 0, len(scopes)+1)
	for _, scope := range scopes {
		parts = append(parts, scope.name)
	}
	return strings.Join(append(parts, csharpGenericName(text, nameStart, nameEnd, ends)), ".")
}

func csharpGenericName(text string, start, end int, ends map[int]int) string {
	name := text[start:end]
	at := scriptSpace(text, end)
	if at >= len(text) || text[at] != '<' {
		return name
	}
	close := scriptTypeArguments(text, at, ends)
	if close < 0 {
		return name
	}
	depth, arity := 0, 1
	for ; at < close; at++ {
		switch text[at] {
		case '<':
			depth++
		case '>':
			depth--
		case ',':
			if depth == 1 {
				arity++
			}
		default:
			if end, ok := ends[at]; ok {
				at = end
			}
		}
	}
	return name + "`" + strconv.Itoa(arity)
}

func partialClassFindings(m *metrics) {
	for name, value := range m.partials {
		for rule, count := range map[string]int{"class_lines": value.lines, "class_methods": value.methods} {
			if m.cancelled() {
				return
			}
			limit := MaxClass
			if rule == "class_methods" {
				limit = MaxMethods
			}
			if count > limit {
				m.add(value.file, value.line, rule, value.name, count, limit)
				m.findings[len(m.findings)-1].key = "csharp_partial\x00" + rule + "\x00" + name
			}
		}
	}
}
