package codequality

import (
	"regexp"
	"strings"
)

var embeddedScript = regexp.MustCompile(`(?is)<script\b[^>]*>`)
var embeddedScriptClose = regexp.MustCompile(`(?is)</script\s*>`)
var inertScriptType = regexp.MustCompile(`(?i)\btype\s*=\s*["']?(?:application/(?:ld\+)?json|importmap|speculationrules)`)

func (m *metrics) embeddedSource(f File) {
	// Keep original byte/line coordinates, measure only executable script
	// regions, and do not interpret markup or JSON data as JavaScript code.
	out := []byte(f.Text)
	scriptBlank(out, 0, len(out))
	for at := 0; at < len(f.Text); {
		open := embeddedScript.FindStringIndex(f.Text[at:])
		if open == nil {
			break
		}
		start, body := at+open[0], at+open[1]
		close := embeddedScriptClose.FindStringIndex(f.Text[body:])
		if close == nil {
			break // Native markup syntax validation remains independently required.
		}
		end := body + close[0]
		if !inertScriptType.MatchString(f.Text[start:body]) {
			copy(out[body:end], f.Text[body:end])
		}
		at = body + close[1]
	}
	if strings.TrimSpace(string(out)) != "" {
		m.javaScriptSource(File{Path: f.Path, Text: string(out)})
	}
}
