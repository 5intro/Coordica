// Package codequality measures source structure without running project code.
// Git, authorization, persistence and model adapters remain outside this package.
package codequality

import (
	"fmt"
	"path"
	"strings"
)

const Version = "maintainability-v1"

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
const Instructions = `Maintainability is a required acceptance gate for every project.
Keep domain rules, application transactions, persistence, transports, Git/process
operations and tool adapters cohesive and separate. Do not create god objects,
unrelated-responsibility services, minified hand-written code or giant functions.
Limits for authored source: 160 Unicode characters per line, 500 lines per file,
80 lines per function, 200 lines per class and 20 methods per class/Go receiver.
Go receiver methods are counted across all files of the same package.
Existing debt is frozen against the fixed target main, never a candidate-edited
allowlist: no new violation or increased existing measurement is acceptable.
Do not evade checks by renaming files/types, moving code into generated/vendor
paths, marking authored code generated, compressing statements or moving one
god object's methods across files. Split by responsibility with small consumer
interfaces, preserving atomic state/audit/idempotency and final version checks.
Plan bounded tasks and include structural cleanup for affected legacy hotspots.
Review cohesion, dependency direction and native language structure even where
automatic syntax metrics are unavailable. Request changes on a violation;
tests passing or an Owner candidate approval does not waive this requirement.
Only Owner may decide a policy change through a separate protected change.
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
		".kt", ".rb", ".php", ".css", ".scss", ".html", ".vue", ".svelte", ".sh", ".ps1":
		return true
	}
	return false
}
