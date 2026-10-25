package codequality

import "strings"

type csharpStringFrame struct {
	quote                               byte
	verbatim, interpolation, expression bool
	braces                              int
}

// Verbatim strings escape quotes by doubling them; a backslash is ordinary
// content. Interpolation expressions re-enter the scanner and retain bodies.
func maskedCSharp(text string) string {
	out := []byte(text)
	var frames []csharpStringFrame
	for at := 0; at < len(text); {
		if len(frames) > 0 {
			f := &frames[len(frames)-1]
			if !f.expression {
				end, done := maskCSharpString(text, out, at, f)
				at = end
				if done {
					frames = frames[:len(frames)-1]
				}
				continue
			}
			if text[at] == '}' && f.braces == 0 {
				f.expression = false
				at++
				continue
			}
		}
		start := at
		if strings.HasPrefix(text[at:], "//") {
			for at < len(text) && text[at] != '\n' {
				at++
			}
			scriptBlank(out, start, at)
			continue
		}
		if strings.HasPrefix(text[at:], "/*") {
			if end := strings.Index(text[at+2:], "*/"); end >= 0 {
				at += end + 4
			} else {
				at = len(text)
			}
			scriptBlank(out, start, at)
			continue
		}
		if size, frame := csharpStringPrefix(text[at:]); size > 0 {
			frames = append(frames, frame)
			at += size
			scriptBlank(out, start, at)
			out[start] = '0'
			continue
		}
		if len(frames) > 0 {
			f := &frames[len(frames)-1]
			if text[at] == '{' {
				f.braces++
			} else if text[at] == '}' {
				f.braces--
			}
		}
		at++
	}
	return string(out)
}

func csharpStringPrefix(text string) (int, csharpStringFrame) {
	for _, prefix := range []string{`$@"`, `@$"`, `@"`, `$"`, `"`, `'`} {
		if strings.HasPrefix(text, prefix) {
			return len(prefix), csharpStringFrame{quote: prefix[len(prefix)-1],
				verbatim: strings.Contains(prefix, "@"), interpolation: strings.Contains(prefix, "$")}
		}
	}
	return 0, csharpStringFrame{}
}

func maskCSharpString(text string, out []byte, at int, f *csharpStringFrame) (int, bool) {
	c := text[at]
	if c == f.quote {
		if f.verbatim && at+1 < len(text) && text[at+1] == f.quote {
			scriptBlank(out, at, at+2)
			return at + 2, false
		}
		scriptBlank(out, at, at+1)
		return at + 1, true
	}
	if !f.verbatim && c == '\\' {
		end := min(len(text), at+2)
		scriptBlank(out, at, end)
		return end, false
	}
	if f.interpolation && (c == '{' || c == '}') {
		if at+1 < len(text) && text[at+1] == c {
			scriptBlank(out, at, at+2)
			return at + 2, false
		}
		if c == '{' {
			f.expression = true
			f.braces = 0
			return at + 1, false
		}
	}
	scriptBlank(out, at, at+1)
	return at + 1, false
}
