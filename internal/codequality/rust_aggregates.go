package codequality

import "sort"

type rustContext struct{ crate, scope string }
type rustAggregate struct {
	part           rustTypePart
	lines, methods int
}

func rustClassFindings(m *metrics) {
	contexts := rustModuleContexts(m)
	aggregates := map[string]*rustAggregate{}
	for _, part := range m.rustParts {
		if m.cancelled() {
			return
		}
		for _, context := range contexts[part.file] {
			scope := rustJoin(context.scope, part.scope)
			name := rustResolveType(part.name, scope, rustScopedImports(m.rustModules[part.file], context.scope))
			key := context.crate + ":" + name
			value := aggregates[key]
			if value == nil {
				value = &rustAggregate{part: part}
				aggregates[key] = value
			}
			value.lines += part.lines
			value.methods += part.methods
		}
	}
	for key, value := range aggregates {
		for rule, count := range map[string]int{"class_lines": value.lines, "class_methods": value.methods} {
			limit := MaxClass
			if rule == "class_methods" {
				limit = MaxMethods
			}
			if count > limit {
				m.add(value.part.file, value.part.line, rule, key, count, limit)
				m.findings[len(m.findings)-1].key = "rust_type\x00" + rule + "\x00" + key
			}
		}
	}
}

func rustScopedImports(unit rustModuleInfo, prefix string) map[string]map[string]string {
	imports := map[string]map[string]string{}
	for scope, names := range unit.imports {
		imports[rustJoin(prefix, scope)] = names
	}
	return imports
}

func rustModuleContexts(m *metrics) map[string][]rustContext {
	children := map[string]map[string]string{}
	incoming := map[string]bool{}
	files := []string{}
	for file, unit := range m.rustModules {
		files = append(files, file)
		children[file] = map[string]string{}
		for scope, stem := range unit.children {
			for _, child := range []string{stem + ".rs", stem + "/mod.rs"} {
				if _, ok := m.rustModules[child]; ok {
					children[file][scope] = child
					incoming[child] = true
				}
			}
		}
	}
	sort.Strings(files)
	contexts := map[string][]rustContext{}
	var visit func(string, rustContext, map[string]bool)
	visit = func(file string, context rustContext, active map[string]bool) {
		if active[file] || m.cancelled() {
			return
		}
		active[file] = true
		defer delete(active, file)
		contexts[file] = append(contexts[file], context)
		for scope, child := range children[file] {
			visit(child, rustContext{context.crate, rustJoin(context.scope, scope)}, active)
		}
	}
	for _, file := range files {
		if !incoming[file] {
			visit(file, rustContext{crate: file}, map[string]bool{})
		}
	}
	for _, file := range files {
		if len(contexts[file]) == 0 {
			visit(file, rustContext{crate: file}, map[string]bool{})
		}
	}
	return contexts
}
