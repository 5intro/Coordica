package codequality

import (
	"regexp"
	"sort"
	"strings"
)

var cppOperator = regexp.MustCompile(`\b(operator\s*(?:\(\s*\)|\[\s*\]|` +
	`(?:new|delete)(?:\s*\[\s*\])?|co_await|[+*/%=!<>&|^~,\-]+))\s*\(`)

func nativeSignatureMatches(text, extension string) [][]int {
	matches := scriptSignature.FindAllStringSubmatchIndex(text, -1)
	if strings.Contains("|.cc|.cpp|.h|.hpp|", "|"+extension+"|") {
		matches = append(matches, cppOperator.FindAllStringSubmatchIndex(text, -1)...)
		sort.SliceStable(matches, func(i, j int) bool {
			return matches[i][0] < matches[j][0] || matches[i][0] == matches[j][0] && matches[i][3] > matches[j][3]
		})
	}
	return matches
}

func nativeParameterClose(text string, at int, ends map[int]int) int {
	at = scriptSpace(text, at)
	if at < len(text) && text[at] == '<' {
		at = scriptTypeArguments(text, at, ends)
		if at < 0 {
			return -1
		}
		at = scriptSpace(text, at)
	}
	if close, ok := ends[at]; ok && text[at] == '(' {
		return close
	}
	return -1
}

func csharpExpressionBody(text string, at int, ends map[int]int) (int, int) {
	close := nativeParameterClose(text, at, ends)
	if close < 0 {
		return -1, -1
	}
	for at = close + 1; at < len(text); at++ {
		if strings.HasPrefix(text[at:], "=>") {
			for end := at + 2; end < len(text); end++ {
				if text[end] == ';' {
					return at, end
				}
				if close, ok := ends[end]; ok {
					end = close
				}
			}
			return -1, -1
		}
		if strings.ContainsRune(";={}", rune(text[at])) {
			return -1, -1
		}
		if close, ok := ends[at]; ok {
			at = close
		}
	}
	return -1, -1
}
