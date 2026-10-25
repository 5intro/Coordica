package codequality

import (
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/token"
	"path"
	"strings"
)

type receiverMethod struct {
	Finding
	condition constraint.Expr
}

// Filename matching names include historical targets, as in Go's syslist.
const knownGoOS = "aix android darwin dragonfly freebsd hurd illumos ios js linux nacl netbsd openbsd plan9 solaris wasip1 windows zos"
const knownGoArch = "386 amd64 amd64p32 arm armbe arm64 arm64be loong64 mips mipsle mips64 mips64le " +
	"mips64p32 mips64p32le ppc ppc64 ppc64le riscv riscv64 s390 s390x sparc sparc64 wasm"

func goSourceCondition(f File, set *token.FileSet, root *ast.File) (constraint.Expr, error) {
	var modern, legacy constraint.Expr
	legacyEnd := goLegacyBuildEnd(f.Text[:set.Position(root.Package).Offset])
	for _, group := range root.Comments {
		for _, comment := range group.List {
			at := set.Position(comment.Pos()).Offset
			if comment.Pos() >= root.Package {
				continue
			}
			lineStart := strings.LastIndexByte(f.Text[:at], '\n') + 1
			if strings.TrimSpace(strings.TrimPrefix(f.Text[lineStart:at], "\ufeff")) != "" {
				continue
			}
			line := comment.Text
			if !constraint.IsGoBuild(line) && !constraint.IsPlusBuild(line) {
				continue
			}
			expr, err := constraint.Parse(line)
			if err != nil {
				return nil, &SourceError{Cause: fmt.Errorf("Go build constraint %s: %w", f.Path, err)}
			}
			if constraint.IsGoBuild(line) {
				if modern != nil {
					return nil, &SourceError{Cause: fmt.Errorf("multiple go:build directives in %s", f.Path)}
				}
				modern = expr
			} else if at < legacyEnd {
				legacy = andBuildConditions(legacy, expr)
			}
		}
	}
	if modern == nil {
		modern = legacy
	}
	name, _, _ := strings.Cut(path.Base(f.Path), ".")
	name = strings.TrimSuffix(name, "_test")
	parts := strings.Split(name, "_")
	if len(parts) > 1 {
		last := parts[len(parts)-1]
		if goNameIn(knownGoOS, last) || goNameIn(knownGoArch, last) {
			modern = andBuildConditions(modern, &constraint.TagExpr{Tag: last})
		}
		if len(parts) > 2 && goNameIn(knownGoArch, last) && goNameIn(knownGoOS, parts[len(parts)-2]) {
			modern = andBuildConditions(modern, &constraint.TagExpr{Tag: parts[len(parts)-2]})
		}
	}
	return modern, nil
}

func goLegacyBuildEnd(header string) int {
	end, offset := 0, 0
	for _, line := range strings.Split(header, "\n") {
		offset += len(line) + 1
		line = strings.TrimSpace(strings.TrimPrefix(line, "\ufeff"))
		if line == "" {
			end = offset
			continue
		}
		if !strings.HasPrefix(line, "//") {
			break
		}
	}
	return end
}

func andBuildConditions(left, right constraint.Expr) constraint.Expr {
	if left == nil {
		return right
	}
	if right == nil {
		return left
	}
	return &constraint.AndExpr{X: left, Y: right}
}

func goNameIn(list, name string) bool { return strings.Contains(" "+list+" ", " "+name+" ") }

func buildConditionTags(expr constraint.Expr, tags map[string]bool) {
	switch x := expr.(type) {
	case *constraint.TagExpr:
		tags[x.Tag] = true
	case *constraint.NotExpr:
		buildConditionTags(x.X, tags)
	case *constraint.AndExpr:
		buildConditionTags(x.X, tags)
		buildConditionTags(x.Y, tags)
	case *constraint.OrExpr:
		buildConditionTags(x.X, tags)
		buildConditionTags(x.Y, tags)
	}
}
