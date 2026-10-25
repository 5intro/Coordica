// Package codequality measures source structure without running project code.
// Git, authorization, persistence and model adapters remain outside this package.
package codequality

import (
	"fmt"
	"path"
	"strings"
)

const Version = "maintainability-v2-advisory"

const (
	MaxLine      = 160
	MaxFile      = 500
	MaxFunction  = 80
	MaxMethods   = 20
	MaxClass     = 200
	MaxFileBytes = 2 << 20
	MaxTreeBytes = 64 << 20
	MaxTreeFiles = 20000
)

// Instructions is the same standing requirement for initialization, planning,
// development and independent review. It grants no scope or policy authority.
const Instructions = `
Maintain cohesive modules and small consumer-owned interfaces. Use these
reference signals, not automatic rejection thresholds: 160 Unicode characters
per line, 500 lines per file, 80 lines per function, 200 lines per class and
20 methods per class/Go receiver. Focus on new large modules and mixed
responsibilities, dependency direction and practical opportunities to split.
Metrics are advisory and may be incomplete; do not invent a language parser
or demand formatting churn merely to hit a number. Use project-native lint,
build/type checks, tests and independent code review. Reviewers use judgment
and request changes for material maintenance risks, not a lexical edge case.
Numeric findings neither reject a candidate nor approve one. Fixed candidate,
scope/authorization, test evidence, process-stop and final version checks
remain mandatory. Existing source is compared to fixed target main so the
report focuses on new or growing risk without an editable allowlist.
`

type File struct {
	Path string
	Text string
}

type Finding struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Rule    string `json:"rule"`
	Subject string `json:"subject"`
	Value   int    `json:"value"`
	Limit   int    `json:"limit"`
	key     string
}

func (f Finding) String() string {
	return fmt.Sprintf("%s:%d: %s %s: %d > %d", f.Path, f.Line, f.Rule, f.Subject, f.Value, f.Limit)
}

// Included uses fixed artifact conventions, never repository-supplied config
// or a model's generated-file comment. Moving authored code is reviewed too.
func Included(name string) bool {
	for _, part := range strings.Split(name, "/") {
		switch part {
		case ".git", "node_modules", "vendor", "third_party", "dist":
			return false
		}
	}
	if strings.HasSuffix(name, ".generated.ts") || strings.HasSuffix(name, ".pb.go") {
		return false
	}
	switch strings.ToLower(path.Ext(name)) {
	case ".go", ".js", ".mjs", ".cjs", ".jsx", ".ts", ".tsx", ".py",
		".java", ".cs", ".rs", ".c", ".cc", ".cpp", ".h", ".hpp", ".swift",
		".kt", ".rb", ".php", ".css", ".scss", ".html", ".vue", ".svelte", ".sh", ".ps1", ".psm1":
		return true
	}
	return false
}
