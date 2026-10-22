package codequality

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"strings"
)

var pythonLambda = regexp.MustCompile(`\blambda\b`)

func pythonLambdas(m *metrics, f File) {
	text := maskedSource(f.Text, true, map[int]int{})
	ends := scriptPairs(text)
	for _, match := range pythonLambda.FindAllStringIndex(text, -1) {
		if m.cancelled() {
			return
		}
		colon := pythonLambdaColon(text, match[1], ends)
		if colon < 0 {
			continue
		}
		end := pythonLambdaEnd(text, colon+1, ends)
		line, last := m.lineAt(match[0]), m.lineAt(end)
		header := strings.LastIndex(text[:match[0]], "\n") + 1
		name := fmt.Sprintf("lambda:%x", sha256.Sum256([]byte(strings.Join(strings.Fields(f.Text[header:colon]), " "))))
		m.add(f.Path, line, "function_lines", name, last-line+1, MaxFunction)
	}
}

func pythonLambdaColon(text string, at int, ends map[int]int) int {
	for ; at < len(text); at++ {
		if end, ok := ends[at]; ok {
			at = end
		} else if text[at] == ':' {
			return at
		} else if text[at] == '\n' {
			return -1
		}
	}
	return -1
}

func pythonLambdaEnd(text string, at int, ends map[int]int) int {
	last := at
	for ; at < len(text); at++ {
		if end, ok := ends[at]; ok {
			last, at = end, end
		} else if text[at] == '\\' && at+1 < len(text) && text[at+1] == '\n' {
			at++
		} else if strings.ContainsRune("\n,;)]}", rune(text[at])) {
			break
		} else if !strings.ContainsRune(" \r\t", rune(text[at])) {
			last = at
		}
	}
	return last
}
