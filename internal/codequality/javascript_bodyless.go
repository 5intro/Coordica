package codequality

import "strings"

func scriptBodylessMemberEnd(text string, at, end int, pairs map[int]int) int {
	close := nativeParameterClose(text, at, pairs)
	if close < 0 {
		return -1
	}
	for at = close + 1; at < end; at++ {
		if text[at] == ';' {
			return at
		}
		if text[at] == '=' && !strings.HasPrefix(text[at:], "=>") || text[at] == '}' {
			return -1
		}
		if close, ok := pairs[at]; ok {
			at = close
		}
	}
	return -1
}
