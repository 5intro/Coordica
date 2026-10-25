package codequality

import "regexp"

var javaNewClass = regexp.MustCompile(`\bnew\s+([A-Za-z_$][\w$.]*)`)

func javaAnonymousClasses(m *metrics, f File, text string, pairs, counts map[int]int) {
	for _, match := range javaNewClass.FindAllStringSubmatchIndex(text, -1) {
		if m.cancelled() {
			return
		}
		end := nativeParameterClose(text, match[1], pairs)
		if end < 0 {
			continue
		}
		open := scriptSpace(text, end+1)
		close, ok := pairs[open]
		if !ok || text[open] != '{' {
			continue
		}
		name := "anonymous " + text[match[2]:match[3]]
		scopedClassFinding(m, f, text, name, "class_lines", match[0], open, m.lineAt(close)-m.lineAt(match[0])+1, MaxClass, pairs)
		scopedClassFinding(m, f, text, name, "class_methods", open, open, counts[open], MaxMethods, pairs)
	}
}
