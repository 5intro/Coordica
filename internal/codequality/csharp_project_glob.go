package codequality

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

type csharpCompileItem struct{ includes, removes, excludes []*regexp.Regexp }

func csharpCompileItemFrom(root string, attrs map[string]string) (csharpCompileItem, error) {
	var result csharpCompileItem
	for name, target := range map[string]*[]*regexp.Regexp{"Include": &result.includes, "Remove": &result.removes, "Exclude": &result.excludes} {
		for _, item := range strings.Split(attrs[name], ";") {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			if strings.ContainsAny(item, "$@%()[]") {
				return result, fmt.Errorf("dynamic Compile %s", name)
			}
			if strings.HasPrefix(item, "/") || strings.Contains(item, ":") {
				return result, fmt.Errorf("external absolute Compile path")
			}
			pattern := path.Clean(path.Join(root, strings.ReplaceAll(item, `\`, "/")))
			if strings.HasPrefix(pattern, "../") {
				return result, fmt.Errorf("Compile path outside repository")
			}
			expression, err := regexp.Compile(csharpGlobExpression(pattern))
			if err != nil {
				return result, err
			}
			*target = append(*target, expression)
		}
	}
	return result, nil
}

func csharpGlobExpression(pattern string) string {
	var expression strings.Builder
	expression.WriteByte('^')
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				i++
				if i+1 < len(pattern) && pattern[i+1] == '/' {
					expression.WriteString("(?:.*/)?")
					i++
				} else {
					expression.WriteString(".*")
				}
			} else {
				expression.WriteString("[^/]*")
			}
		case '?':
			expression.WriteString("[^/]")
		default:
			expression.WriteString(regexp.QuoteMeta(string(pattern[i])))
		}
	}
	expression.WriteByte('$')
	return expression.String()
}

func csharpMatches(patterns []*regexp.Regexp, name string) bool {
	for _, pattern := range patterns {
		if pattern.MatchString(name) {
			return true
		}
	}
	return false
}
