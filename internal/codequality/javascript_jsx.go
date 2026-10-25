package codequality

import "strings"

type jsxFrame struct {
	mode, resume     byte
	elements, braces int
	closing          bool
}

type jsxMask []jsxFrame

// JSX child text and quoted attributes are not JavaScript strings. Expressions
// re-enter the ordinary scanner, including nested JSX, templates and callbacks.
func (j *jsxMask) step(text string, out []byte, at int, expression bool) (int, bool) {
	if (len(*j) == 0 || (*j)[len(*j)-1].mode == 'e') && expression && jsxStart(text, at) {
		*j = append(*j, jsxFrame{mode: 't'})
	}
	if len(*j) == 0 {
		return at, false
	}
	f := &(*j)[len(*j)-1]
	c := text[at]
	if f.mode == 'e' {
		if c != '}' || f.braces != 0 {
			return at, false
		}
		f.mode = f.resume
		return at + 1, true
	}
	if c == '{' {
		f.resume, f.mode, f.braces = f.mode, 'e', 0
		return at + 1, true
	}
	if f.mode == 'c' {
		if c != '<' {
			scriptBlank(out, at, at+1)
			return at + 1, true
		}
		f.mode = 't'
	}
	if c == '<' {
		f.closing = strings.HasPrefix(text[at:], "</")
		if !f.closing {
			f.elements++
		}
	}
	if c == '\'' || c == '"' {
		end := scriptQuoteEnd(text, at)
		scriptBlank(out, at, end)
		return end, true
	}
	if c == '>' {
		if f.closing || at > 0 && text[at-1] == '/' {
			f.elements--
		}
		f.mode = 'c'
		if f.elements == 0 {
			*j = (*j)[:len(*j)-1]
		}
	}
	return at + 1, true
}

func (j jsxMask) inExpression() bool {
	return len(j) > 0 && j[len(j)-1].mode == 'e'
}

func (j jsxMask) brace(c byte) {
	if !j.inExpression() {
		return
	}
	f := &j[len(j)-1]
	if c == '{' {
		f.braces++
	} else if c == '}' {
		f.braces--
	}
}

func jsxStart(text string, at int) bool {
	if text[at] != '<' || at+1 >= len(text) {
		return false
	}
	c := text[at+1]
	if c == '>' {
		return true
	}
	if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z') {
		return false
	}
	// TSX generic arrows use a trailing comma or an extends constraint. Neither
	// is a JSX tag declaration. Do not consume the following executable body.
	for i := at + 2; i < len(text); i++ {
		switch text[i] {
		case '\'', '"', '{':
			return !strings.Contains(text[at:i], " extends ")
		case ',':
			return false
		case '>':
			return !strings.Contains(text[at:i], " extends ")
		case ';':
			return false
		}
	}
	return false
}
