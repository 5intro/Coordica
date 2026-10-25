package codequality

import (
	"regexp"
	"strings"
)

var embeddedScriptClose = regexp.MustCompile(`(?is)</script\s*>`)

func (m *metrics) embeddedSource(f File) {
	// Keep original byte/line coordinates, measure only executable script
	// regions, and do not interpret markup or JSON data as JavaScript code.
	out := []byte(f.Text)
	scriptBlank(out, 0, len(out))
	for at := 0; at < len(f.Text); {
		start := markupScriptStart(f.Text, at)
		if start < 0 {
			break
		}
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

// Walk markup tags and comments without entering quoted attributes. Once a
// script begins, its raw text is handled separately (including literal <!--).
func markupScriptStart(text string, at int) int {
	for at < len(text) {
		next := strings.IndexByte(text[at:], '<')
		if next < 0 {
			return -1
		}
		at += next
		if strings.HasPrefix(text[at:], "<!--") {
			end := strings.Index(text[at+4:], "-->")
			if end < 0 {
				return -1
			}
			at += 4 + end + 3
			continue
		}
		end := at + 1
		for end < len(text) && (scriptWord(text[end]) || text[end] == ':' || text[end] == '-') {
			end++
		}
		if end == at+1 {
			at++
			continue
		}
		if strings.EqualFold(text[at+1:end], "script") {
			return end
		}
		if close := scriptTagEnd(text, end); close >= 0 {
			at = close
		} else {
			return -1
		}
	}
	return -1
}
