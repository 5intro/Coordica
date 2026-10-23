package codequality

import "context"

// Attribute each already ordered declaration once while walking the source.
// Re-scanning all brace pairs for every class/function was cubic on dense input.
func nativeMethodCounts(ctx context.Context, text string, functions []nativeBlock) map[int]int {
	counts := map[int]int{}
	var stack []int
	index := 0
	for at := 0; at < len(text); at++ {
		if ctx != nil && ctx.Err() != nil {
			return counts
		}
		for index < len(functions) && functions[index].start == at {
			if len(stack) > 0 {
				counts[stack[len(stack)-1]]++
			}
			index++
		}
		if text[at] == '{' {
			stack = append(stack, at)
		} else if text[at] == '}' && len(stack) > 0 {
			stack = stack[:len(stack)-1]
		}
	}
	return counts
}
