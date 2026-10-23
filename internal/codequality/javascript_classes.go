package codequality

func scriptClassBody(text string, at int, ends map[int]int) int {
	for at < len(text) {
		if text[at] == '{' {
			return at
		}
		if text[at] == ';' {
			return -1
		}
		if close, ok := ends[at]; ok {
			at = close + 1
		} else if text[at] == '<' {
			at = scriptTypeArguments(text, at, ends)
			if at < 0 {
				return -1
			}
		} else {
			at++
		}
	}
	return -1
}
