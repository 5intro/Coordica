package codequality

func scriptBlank(out []byte, start, end int) {
	for i := start; i < end; i++ {
		if out[i] != '\n' {
			out[i] = ' '
		}
	}
}

// Literal text is inert, but ${...} is ordinary executable JavaScript.
// An explicit interpolation stack in the caller supports nested templates
// without recursion or a source-controlled parser stack limit.
func maskScriptTemplate(text string, out []byte, at int) (int, bool) {
	for at < len(text) {
		start := at
		switch {
		case text[at] == '\\':
			at = min(len(text), at+2)
		case text[at] == '`':
			scriptBlank(out, at, at+1)
			return at + 1, false
		case text[at] == '$' && at+1 < len(text) && text[at+1] == '{':
			out[at] = ' '
			return at + 2, true // Retain the balanced interpolation braces.
		default:
			at++
		}
		scriptBlank(out, start, at)
	}
	return at, false
}

func scriptTemplateEnd(text string, start int, ends map[int]int) int {
	for at := start + 1; at < len(text); at++ {
		if text[at] == '\\' {
			at++
		} else if text[at] == '`' {
			return at + 1
		} else if text[at] == '$' && at+1 < len(text) && text[at+1] == '{' {
			if close, ok := ends[at+1]; ok {
				at = close
			}
		}
	}
	return len(text)
}
