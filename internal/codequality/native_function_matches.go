package codequality

import (
	"regexp"
	"sort"
	"strings"
)

var swiftInitializer = regexp.MustCompile(`\b(init[?!]?)\s*(?:<|\()`)

func nativeFunctionMatches(text, extension string) ([][]int, bool) {
	keyword := extension == ".rs" || extension == ".swift" || extension == ".kt" || extension == ".php"
	if !keyword {
		return nativeSignatureMatches(text, extension), false
	}
	if extension == ".kt" {
		return kotlinFunction.FindAllStringSubmatchIndex(text, -1), true
	}
	matches := nativeFunction.FindAllStringSubmatchIndex(text, -1)
	if extension == ".swift" {
		for _, match := range swiftInitializer.FindAllStringSubmatchIndex(text, -1) {
			at := match[0] - 1
			for at >= 0 && strings.ContainsRune(" \t\r\n", rune(text[at])) {
				at--
			}
			if at >= 0 && text[at] == '.' {
				continue
			}
			matches = append(matches, match)
		}
		sort.SliceStable(matches, func(i, j int) bool { return matches[i][0] < matches[j][0] })
	}
	return matches, true
}
