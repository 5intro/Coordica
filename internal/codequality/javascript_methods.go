package codequality

import (
	"context"
	"sort"
	"strings"
)

// Computed names are balanced expressions, including nested calls/indexing.
// Keep their full original span as identity instead of the masked string value.
func scriptMethodMatches(ctx context.Context, original, text string, ends map[int]int) [][]int {
	matches := scriptSignature.FindAllStringSubmatchIndex(text, -1)
	for open, close := range ends {
		if ctx != nil && ctx.Err() != nil {
			return nil
		}
		if text[open] != '[' {
			continue
		}
		at := scriptSpace(text, close+1)
		if at < len(text) && (text[at] == '(' || text[at] == '<') {
			matches = append(matches, []int{open, at + 1, open, close + 1})
		}
	}
	for at := 0; at < len(text); at++ {
		if ctx != nil && ctx.Err() != nil {
			return nil
		}
		// The mask marks real literals, so quotes inside comments/JSX text cannot
		// create declarations. Preserve the entire authored property-name span.
		if text[at] != '0' || !strings.ContainsRune("'\"", rune(original[at])) {
			continue
		}
		close := scriptQuoteEnd(original, at)
		next := scriptSpace(text, close)
		if next < len(text) && (text[next] == '(' || text[next] == '<') {
			matches = append(matches, []int{at, next + 1, at, close})
		}
		at = close - 1
	}
	sort.SliceStable(matches, func(i, j int) bool { return matches[i][0] < matches[j][0] })
	return matches
}
