package codequality

import (
	"path"
	"regexp"
	"strings"
)

var shellFunction = regexp.MustCompile(`(?m)(?:^|[;\n])\s*(?:function\s+([\w-]+)\s*(?:\(\s*\))?|([\w-]+)\s*\(\s*\))\s*\{`)
var powerFunction = regexp.MustCompile(`(?i)\bfunction\s+([\w:-]+)\s*(?:\([^{}]*\))?\s*\{`)
var scssFunction = regexp.MustCompile(`@(?:function|mixin)\s+([\w-]+)\s*\(`)
var powerClass = regexp.MustCompile(`(?i)\bclass\s+([\w]+)[^;{}]*\{`)

func (m *metrics) commandSource(f File) {
	extension := strings.ToLower(path.Ext(f.Path))
	text := maskedSource(f.Text, extension != ".scss", map[int]int{})
	ends := scriptPairs(text)
	pattern := shellFunction
	if extension == ".ps1" {
		pattern = powerFunction
	} else if extension == ".scss" {
		pattern = scssFunction
	}
	for _, match := range pattern.FindAllStringSubmatchIndex(text, -1) {
		nameStart, nameEnd := match[2], match[3]
		if nameStart < 0 {
			nameStart, nameEnd = match[4], match[5]
		}
		body := match[1] - 1
		if extension == ".scss" {
			body = nativeFunctionBody(text, nameEnd, ends)
		}
		m.blockFunction(f.Path, text, text[nameStart:nameEnd], match[0], body, ends)
	}
	if extension == ".ps1" {
		m.powerClasses(f, text, ends)
	}
}

func (m *metrics) powerClasses(f File, text string, ends map[int]int) {
	functions := nativeFunctions(text, ".cs", ends)
	for _, match := range powerClass.FindAllStringSubmatchIndex(text, -1) {
		open := match[1] - 1
		if close, ok := ends[open]; ok {
			name, methods := text[match[2]:match[3]], 0
			for _, fn := range functions {
				if enclosingBrace(braceEnds(text), fn.start) == open {
					methods++
					m.blockFunction(f.Path, text, name+"."+fn.name, fn.start, fn.body, ends)
				}
			}
			m.add(f.Path, lineAt(text, match[0]), "class_lines", name, lineAt(text, close)-lineAt(text, match[0])+1, MaxClass)
			m.add(f.Path, lineAt(text, open), "class_methods", name, methods, MaxMethods)
		}
	}
}
