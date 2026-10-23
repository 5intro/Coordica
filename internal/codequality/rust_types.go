package codequality

import (
	"path"
	"regexp"
	"sort"
	"strings"
)

var rustModule = regexp.MustCompile(`\bmod\s+([A-Za-z_]\w*)\s*([;{])`)
var rustUse = regexp.MustCompile(`\buse\s+([^;]+);`)
var rustQualifiedType = regexp.MustCompile(`^(?:(?:[A-Za-z_]\w*)::)*[A-Za-z_]\w*`)

type rustTypePart struct {
	name, scope string
	file        string
	line, lines int
	methods     int
}

type rustModuleInfo struct {
	children map[string]string
	imports  map[string]map[string]string
	scopes   []rustScopeRange
}

type rustScopeRange struct {
	start, end, parent int
	name               string
}

func rustScopes(text string, pairs map[int]int) []rustScopeRange {
	var scopes []rustScopeRange
	parent := -1
	for _, match := range rustModule.FindAllStringSubmatchIndex(text, -1) {
		open := match[1] - 1
		if text[open] != '{' || pairs[open] == 0 {
			continue
		}
		for parent >= 0 && scopes[parent].end < open {
			parent = scopes[parent].parent
		}
		name := text[match[2]:match[3]]
		if parent >= 0 {
			name = rustJoin(scopes[parent].name, name)
		}
		scopes = append(scopes, rustScopeRange{open, pairs[open], parent, name})
		parent = len(scopes) - 1
	}
	return scopes
}

func rustScope(unit rustModuleInfo, at int) string {
	index := sort.Search(len(unit.scopes), func(i int) bool { return unit.scopes[i].start >= at }) - 1
	for index >= 0 {
		if unit.scopes[index].end > at {
			return unit.scopes[index].name
		}
		index = unit.scopes[index].parent
	}
	return ""
}

func recordRustFile(m *metrics, f File, text string, pairs map[int]int) {
	if m.rustModules == nil {
		m.rustModules = map[string]rustModuleInfo{}
	}
	unit := rustModuleInfo{children: map[string]string{}, imports: map[string]map[string]string{}, scopes: rustScopes(text, pairs)}
	for _, match := range rustModule.FindAllStringSubmatchIndex(text, -1) {
		if text[match[1]-1] != ';' {
			continue
		}
		scope := rustScope(unit, match[0])
		name := text[match[2]:match[3]]
		unit.children[rustJoin(scope, name)] = rustChildFile(f.Path, scope, name)
	}
	for _, match := range rustUse.FindAllStringSubmatchIndex(text, -1) {
		scope := rustScope(unit, match[0])
		if unit.imports[scope] == nil {
			unit.imports[scope] = map[string]string{}
		}
		rustImportNames(text[match[2]:match[3]], "", unit.imports[scope])
	}
	m.rustModules[f.Path] = unit
}

func rustChildFile(file, scope, name string) string {
	dir, base := path.Dir(file), path.Base(file)
	if base != "lib.rs" && base != "main.rs" && base != "mod.rs" {
		dir = path.Join(dir, strings.TrimSuffix(base, ".rs"))
	}
	return path.Join(dir, strings.ReplaceAll(scope, "::", "/"), name)
}

func rustJoin(prefix, name string) string {
	if name == "" {
		return prefix
	}
	if prefix == "" {
		return name
	}
	return prefix + "::" + name
}

func recordRustType(m *metrics, f File, text string, match []int, open, close, methods int, pairs map[int]int) {
	header := strings.TrimSpace(text[match[0]:open])
	name := text[match[2]:match[3]]
	if strings.HasPrefix(header, "impl") {
		header = strings.TrimSpace(strings.TrimPrefix(header, "impl"))
		if strings.HasPrefix(header, "<") {
			if end := scriptTypeArguments(header, 0, scriptPairs(header)); end >= 0 {
				header = strings.TrimSpace(header[end:])
			}
		}
		if at := strings.Index(header, " for "); at >= 0 {
			header = strings.TrimSpace(header[at+5:])
		}
		name = rustQualifiedType.FindString(header)
	}
	if name == "" {
		return
	}
	m.rustParts = append(m.rustParts, rustTypePart{name: name, scope: rustScope(m.rustModules[f.Path], match[0]), file: f.Path,
		line: m.lineAt(match[0]), lines: m.lineAt(close) - m.lineAt(match[0]) + 1, methods: methods})
}
