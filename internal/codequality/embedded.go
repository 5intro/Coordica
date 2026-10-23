package codequality

import (
	"regexp"
	"strings"
)

var embeddedScript = regexp.MustCompile(`(?is)<script\b`)
var embeddedScriptClose = regexp.MustCompile(`(?is)</script\s*>`)

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
		start := at + open[1]
		body := scriptTagEnd(f.Text, start)
		if body < 0 {
			break
		}
		close := embeddedScriptClose.FindStringIndex(f.Text[body:])
		if close == nil {
			break // Native markup syntax validation remains independently required.
		}
		end := body + close[0]
		if !inertScriptAttributes(f.Text[start:body]) {
			copy(out[body:end], f.Text[body:end])
		}
		at = body + close[1]
	}
	if strings.TrimSpace(string(out)) != "" {
		m.javaScriptSource(File{Path: f.Path, Text: string(out)})
	}
}
