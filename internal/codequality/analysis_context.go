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
	if csharpMetadata(f.Path) {
		return nil
	}
	m.csharpScopes = nil
	if strings.EqualFold(path.Ext(f.Path), ".cs") {
		var err error
		m.csharpScopes, err = csharpSourceScopes(m.csharpProjects, f.Path)
		if err != nil {
			return err
		}
	}
	m.lines = sourceLineOffsets(f.Text)
	m.text(f)
	if m.cancelled() {
		return m.ctx.Err()
	}
	if strings.EqualFold(path.Ext(f.Path), ".go") {
		if err := m.goSource(f); err != nil {
			return err
		}
	} else {
		f.Text = physicalNewlines(f.Text)
		m.lines = sourceLineOffsets(f.Text)
		m.structuredSource(f)
	}
	return m.ctx.Err()
}
