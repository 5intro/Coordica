package codequality

import (
	"crypto/sha256"
	"fmt"
	"path"
	"sort"
	"strings"
	"unicode/utf8"
)

type metrics struct {
	findings []Finding
	methods  map[string][]Finding
}

func (m *metrics) add(file string, line int, rule, subject string, value, limit int) {
	if value > limit {
		m.findings = append(m.findings, Finding{
			Path: file, Line: line, Rule: rule, Subject: subject,
			Value: value, Limit: limit, key: file + "\x00" + rule + "\x00" + subject,
		})
	}
}

// Measure returns only violations. Errors fail closed rather than silently
// skipping malformed or unbounded source. It does not execute a formatter.
func Measure(files []File) ([]Finding, error) {
	m := metrics{methods: map[string][]Finding{}}
	total := 0
	seen := map[string]bool{}
	for _, f := range files {
		if !Included(f.Path) {
			continue
		}
		total += len(f.Text)
		if seen[f.Path] || len(f.Text) > MaxFileBytes || total > MaxTreeBytes || len(seen) >= MaxTreeFiles {
			return nil, fmt.Errorf("source inventory duplicate or size limit at %s", f.Path)
		}
		seen[f.Path] = true
		if !utf8.ValidString(f.Text) || strings.ContainsRune(f.Text, 0) {
			return nil, fmt.Errorf("source is not UTF-8 text: %s", f.Path)
		}
		m.text(f)
		if path.Ext(f.Path) == ".go" {
			if err := m.goSource(f); err != nil {
				return nil, err
			}
		} else {
			m.structuredSource(f)
		}
	}
	for key, methods := range m.methods {
		f := methods[0]
		m.add(f.Path, f.Line, "receiver_methods", key, len(methods), MaxMethods)
	}
	sort.Slice(m.findings, func(i, j int) bool {
		a, b := m.findings[i], m.findings[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.key < b.key
	})
	return m.findings, nil
}

func (m *metrics) text(f File) {
	lines := strings.Split(strings.TrimSuffix(f.Text, "\n"), "\n")
	if f.Text == "" {
		lines = nil
	}
	m.add(f.Path, 1, "file_lines", "file", len(lines), MaxFile)
	for i, line := range lines {
		line = strings.TrimSuffix(line, "\r")
		// Content identity makes exemptions survive a line-number shift, while
		// edited/duplicated long lines must be fixed instead of inheriting debt.
		subject := fmt.Sprintf("%x", sha256.Sum256([]byte(line)))
		m.add(f.Path, i+1, "line_length", subject, utf8.RuneCountInString(line), MaxLine)
	}
}

// Regressions freezes each known violation against actual fixed main source.
// A duplicate finding consumes a separate allowance. Renaming is new debt.
func Regressions(base, candidate []File) ([]Finding, error) {
	old, err := Measure(base)
	if err != nil {
		return nil, fmt.Errorf("baseline quality inventory: %w", err)
	}
	next, err := Measure(candidate)
	if err != nil {
		return nil, fmt.Errorf("candidate quality inventory: %w", err)
	}
	allowances := map[string][]int{}
	for _, f := range old {
		allowances[f.key] = append(allowances[f.key], f.Value)
	}
	var failures []Finding
	for _, f := range next {
		values := allowances[f.key]
		match := -1
		for i, value := range values {
			if f.Value <= value && (match == -1 || value < values[match]) {
				match = i
			}
		}
		if match == -1 {
			failures = append(failures, f)
		} else {
			allowances[f.key] = append(values[:match], values[match+1:]...)
		}
	}
	return failures, nil
}
