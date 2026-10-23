package codequality

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

type metrics struct {
	ctx      context.Context
	lines    []int
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
	return MeasureContext(context.Background(), files)
}

func MeasureContext(ctx context.Context, files []File) ([]Finding, error) {
	return measureInventory(ctx, files, false)
}

func measureInventory(ctx context.Context, files []File, baseline bool) ([]Finding, error) {
	m := metrics{ctx: ctx, methods: map[string][]Finding{}}
	total := 0
	seen := map[string]bool{}
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !Included(f.Path) {
			continue
		}
		total += len(f.Text)
		if seen[f.Path] || len(f.Text) > MaxFileBytes || total > MaxTreeBytes || len(seen) >= MaxTreeFiles {
			return nil, fmt.Errorf("source inventory duplicate or size limit at %s", f.Path)
		}
		seen[f.Path] = true
		if !utf8.ValidString(f.Text) || strings.ContainsRune(f.Text, 0) {
			if !baseline {
				return nil, &SourceError{Cause: fmt.Errorf("source is not UTF-8 text: %s", f.Path)}
			}
			m.text(f)
			continue
		}
		if err := measureSource(&m, f); err != nil {
			var source *SourceError
			if !baseline || !errors.As(err, &source) {
				return nil, err
			}
		}
	}
	for key, methods := range m.methods {
		if m.cancelled() {
			return nil, ctx.Err()
		}
		f := methods[0]
		m.add(f.Path, f.Line, "receiver_methods", key, len(methods), MaxMethods)
		if len(methods) > MaxMethods {
			// Location is diagnostic only; receiver debt belongs to its package
			// and type, even after its first method/file is removed.
			m.findings[len(m.findings)-1].key = "receiver_methods\x00" + key
		}
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
	return m.findings, ctx.Err()
}

func (m *metrics) text(f File) {
	lines := strings.Split(strings.TrimSuffix(f.Text, "\n"), "\n")
	if f.Text == "" {
		lines = nil
	}
	m.add(f.Path, 1, "file_lines", "file", len(lines), MaxFile)
	for i, line := range lines {
		if m.cancelled() {
			return
		}
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
	return RegressionsContext(context.Background(), base, candidate)
}

func RegressionsContext(ctx context.Context, base, candidate []File) ([]Finding, error) {
	old, err := measureInventory(ctx, base, true)
	if err != nil {
		return nil, fmt.Errorf("baseline quality inventory: %w", err)
	}
	next, err := MeasureContext(ctx, candidate)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, &CandidateError{Cause: err}
	}
	allowances := map[string][]int{}
	for _, f := range old {
		allowances[f.key] = append(allowances[f.key], f.Value)
	}
	var failures []Finding
	for _, f := range next {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
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
