package codequality

import (
	"fmt"
	"strings"
)

// Summary is bounded material for independent review, never an authority or
// acceptance result. Labels originate in source and must remain untrusted data.
func Summary(findings []Finding) string {
	if len(findings) == 0 {
		return ""
	}
	var out strings.Builder
	fmt.Fprintf(&out, "%s: %d advisory risk signals; review with judgment.", Version, len(findings))
	for _, f := range findings[:min(8, len(findings))] {
		subject := f.Subject
		if f.Rule == "line_length" {
			subject = "line"
		}
		line := fmt.Sprintf("\n%q:%d %s %q: %d (reference %d)", shortLabel(f.Path), f.Line, f.Rule, shortLabel(subject), f.Value, f.Limit)
		if out.Len()+len(line) > 8192 {
			break
		}
		out.WriteString(line)
	}
	return out.String()
}

func shortLabel(value string) string {
	runes := []rune(value)
	if len(runes) > 512 {
		return string(runes[:512]) + "…"
	}
	return value
}
