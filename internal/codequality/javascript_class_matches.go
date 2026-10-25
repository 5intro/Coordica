package codequality

import (
	"path"
	"regexp"
	"sort"
	"strings"
)

var interfaceDeclaration = regexp.MustCompile(`\binterface\s+([A-Za-z_$][\w$]*)`)

func scriptClassMatches(file, text string) [][]int {
	matches := classBlock.FindAllStringSubmatchIndex(text, -1)
	if extension := strings.ToLower(path.Ext(file)); extension == ".ts" || extension == ".tsx" {
		matches = append(matches, interfaceDeclaration.FindAllStringSubmatchIndex(text, -1)...)
		sort.SliceStable(matches, func(i, j int) bool { return matches[i][0] < matches[j][0] })
	}
	return matches
}
