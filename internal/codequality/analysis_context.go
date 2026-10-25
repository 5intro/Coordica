package codequality

import (
	"path"
	"sort"
	"strings"
)

func sourceLineOffsets(text string) []int {
	lines := []int{0}
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' || text[i] == '\r' && (i+1 == len(text) || text[i+1] != '\n') {
			lines = append(lines, i+1)
		}
	}
	return lines
}

func physicalNewlines(text string) string {
	return strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
}

func indexedLine(lines []int, offset int) int {
	return sort.Search(len(lines), func(i int) bool { return lines[i] > offset })
}

func (m *metrics) lineAt(offset int) int { return indexedLine(m.lines, offset) }

func (m *metrics) cancelled() bool { return m.ctx != nil && m.ctx.Err() != nil }

func measureSource(m *metrics, f File) error {
	m.lines = sourceLineOffsets(f.Text)
	m.text(f)
	if m.cancelled() {
		return m.ctx.Err()
	}
	// All languages get cheap physical signals. Go uses the standard library
	// parser; other language semantics belong to existing native tools/review.
	if strings.EqualFold(path.Ext(f.Path), ".go") {
		return m.goSource(f)
	}
	return m.ctx.Err()
}
