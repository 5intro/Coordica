package codequality

import "strings"

func rustImportNames(text, prefix string, names map[string]string) {
	text = strings.TrimSpace(text)
	if at := strings.IndexByte(text, '{'); at >= 0 {
		close := strings.LastIndexByte(text, '}')
		if close <= at {
			return
		}
		prefix += text[:at]
		depth, start := 0, at+1
		for i := start; i <= close; i++ {
			if i == close || text[i] == ',' && depth == 0 {
				rustImportNames(text[start:i], prefix, names)
				start = i + 1
			} else if text[i] == '{' {
				depth++
			} else if text[i] == '}' {
				depth--
			}
		}
		return
	}
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return
	}
	target := prefix + fields[0]
	parts := strings.Split(target, "::")
	name := parts[len(parts)-1]
	if name == "self" && len(parts) > 1 {
		parts = parts[:len(parts)-1]
		target, name = strings.Join(parts, "::"), parts[len(parts)-1]
	}
	if len(fields) == 3 && fields[1] == "as" {
		name = fields[2]
	}
	if name != "*" && name != "_" {
		names[name] = target
	}
}

func rustResolveType(name, scope string, imports map[string]map[string]string) string {
	parts := strings.Split(name, "::")
	if imported := imports[scope][parts[0]]; imported != "" {
		name = imported + strings.TrimPrefix(name, parts[0])
	}
	parts = strings.Split(name, "::")
	base := []string{}
	if scope != "" {
		base = strings.Split(scope, "::")
	}
	if parts[0] == "crate" {
		base, parts = nil, parts[1:]
	}
	for len(parts) > 0 && (parts[0] == "self" || parts[0] == "super") {
		if parts[0] == "super" && len(base) > 0 {
			base = base[:len(base)-1]
		}
		parts = parts[1:]
	}
	return strings.Join(append(base, parts...), "::")
}
