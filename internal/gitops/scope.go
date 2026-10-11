package gitops

import (
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"unicode/utf8"
)

// ErrInvalidPath identifies paths that are not unambiguous repository-relative
// paths. Validation preserves accepted paths rather than cleaning them.
var ErrInvalidPath = errors.New("invalid repository path")

// ValidatePath checks a UTF-8, slash-separated repository-relative path.
// Tabs, newlines, spaces, and Unicode are allowed in path components.
func ValidatePath(name string) error {
	if name == "" {
		return fmt.Errorf("%w: empty path", ErrInvalidPath)
	}
	if !utf8.ValidString(name) {
		return fmt.Errorf("%w: invalid UTF-8", ErrInvalidPath)
	}
	if strings.ContainsAny(name, "\\\x00") {
		return fmt.Errorf("%w: backslash or NUL", ErrInvalidPath)
	}
	if strings.HasPrefix(name, "/") || hasDrivePrefix(name) {
		return fmt.Errorf("%w: absolute or drive-relative path", ErrInvalidPath)
	}
	for _, component := range strings.Split(name, "/") {
		if component == "" || component == "." || component == ".." {
			return fmt.Errorf("%w: empty, dot, or dot-dot component", ErrInvalidPath)
		}
	}
	return nil
}

func hasDrivePrefix(name string) bool {
	return len(name) >= 2 && name[1] == ':' &&
		((name[0] >= 'a' && name[0] <= 'z') || (name[0] >= 'A' && name[0] <= 'Z'))
}

// ScopeResult reports authorization and protected paths separately. Matching a
// protected pattern never makes an otherwise out-of-scope path authorized.
type ScopeResult struct {
	Allowed    bool
	OutOfScope []string
	Protected  []string
}

// Scope matches repository paths against allow and protection patterns.
type Scope struct {
	allowed   [][]string
	protected [][]string
}

// NewScope accepts exact paths, path.Match globs within individual components,
// and ** as an entire component matching zero or more components. A trailing /
// matches descendants of a directory, but does not match the directory itself.
// Backslash escapes are not supported.
func NewScope(allowed, protected []string) (*Scope, error) {
	a, err := compilePatterns(allowed)
	if err != nil {
		return nil, fmt.Errorf("allowed scope: %w", err)
	}
	p, err := compilePatterns(protected)
	if err != nil {
		return nil, fmt.Errorf("protected scope: %w", err)
	}
	return &Scope{allowed: a, protected: p}, nil
}

func compilePatterns(patterns []string) ([][]string, error) {
	compiled := make([][]string, 0, len(patterns))
	for _, pattern := range patterns {
		directory := strings.HasSuffix(pattern, "/")
		name := strings.TrimSuffix(pattern, "/")
		if err := ValidatePath(name); err != nil {
			return nil, fmt.Errorf("pattern %q: %w", pattern, err)
		}
		components := strings.Split(name, "/")
		for _, component := range components {
			if _, err := path.Match(component, ""); err != nil {
				return nil, fmt.Errorf("pattern %q: %w", pattern, err)
			}
		}
		if directory {
			// At least one component must follow the directory pattern.
			components = append(components, "*", "**")
		}
		compiled = append(compiled, components)
	}
	return compiled, nil
}

// Check returns sorted, deduplicated findings. Invalid paths are out of scope.
// Allowed means every input path matches an allow pattern; protected findings
// remain available for a separate approval decision.
func (s *Scope) Check(paths []string) ScopeResult {
	result := ScopeResult{Allowed: true}
	outOfScope := make(map[string]struct{})
	protected := make(map[string]struct{})
	for _, name := range paths {
		if ValidatePath(name) != nil {
			outOfScope[name] = struct{}{}
			continue
		}
		components := strings.Split(name, "/")
		if !matchesAny(s.allowed, components) {
			outOfScope[name] = struct{}{}
		}
		if matchesAny(s.protected, components) {
			protected[name] = struct{}{}
		}
	}
	result.OutOfScope = sortedKeys(outOfScope)
	result.Protected = sortedKeys(protected)
	result.Allowed = len(result.OutOfScope) == 0
	return result
}

func matchesAny(patterns [][]string, components []string) bool {
	for _, pattern := range patterns {
		if matchesComponents(pattern, components) {
			return true
		}
	}
	return false
}

func matchesComponents(pattern, components []string) bool {
	// reachable[i] means that the processed pattern matches the first i path
	// components. Dynamic programming avoids exponential ** backtracking.
	reachable := make([]bool, len(components)+1)
	reachable[0] = true
	for _, component := range pattern {
		if component == "**" {
			for i := 1; i < len(reachable); i++ {
				reachable[i] = reachable[i] || reachable[i-1]
			}
			continue
		}
		next := make([]bool, len(reachable))
		for i, name := range components {
			if reachable[i] {
				next[i+1], _ = path.Match(component, name)
			}
		}
		reachable = next
	}
	return reachable[len(components)]
}

func sortedKeys(set map[string]struct{}) []string {
	if len(set) == 0 {
		return nil
	}
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
