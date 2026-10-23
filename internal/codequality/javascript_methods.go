package codequality

import (
	"context"
	"sort"
)

// Computed names are balanced expressions, including nested calls/indexing.
// Keep their full original span as identity instead of the masked string value.
func scriptMethodMatches(ctx context.Context, text string, ends map[int]int) [][]int {
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
	sort.SliceStable(matches, func(i, j int) bool { return matches[i][0] < matches[j][0] })
	return matches
}
