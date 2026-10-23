package codequality

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
)

func scriptHeaderStart(text string, at int, starts map[int]int) int {
	for i := at - 1; i >= 0; i-- {
		if open, ok := starts[i]; ok {
			i = open
			continue
		}
		if strings.ContainsRune(";{}", rune(text[i])) {
			return i + 1
		}
	}
	return 0
}

// Binding/call-site headers and enclosing scopes distinguish anonymous
// functions without using mutable line numbers or body length as identity.
func scriptIdentity(ctx context.Context, original, text string, start, headerEnd int, ends map[int]int) string {
	starts := map[int]int{}
	var scopes []int
	for open, close := range ends {
		if ctx != nil && ctx.Err() != nil {
			return ""
		}
		starts[close] = open
		if text[open] == '{' && open < start && close >= headerEnd {
			scopes = append(scopes, open)
		}
	}
	sort.Ints(scopes)
	var context strings.Builder
	for _, open := range append(scopes, headerEnd) {
		if ctx != nil && ctx.Err() != nil {
			return ""
		}
		begin := scriptHeaderStart(text, open, starts)
		context.WriteString(strings.Join(strings.Fields(original[begin:open]), " "))
		context.WriteByte(0)
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(context.String())))
}

func (m *metrics) scriptFunctionFinding(f File, text, name string, start, headerEnd, end int, ends map[int]int) {
	value := m.lineAt(end) - m.lineAt(start) + 1
	if value <= MaxFunction {
		return
	}
	m.add(f.Path, m.lineAt(start), "function_lines", name, value, MaxFunction)
	finding := &m.findings[len(m.findings)-1]
	finding.key += "\x00" + scriptIdentity(m.ctx, f.Text, text, start, headerEnd, ends)
}
