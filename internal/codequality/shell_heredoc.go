package codequality

import "strings"

type shellDocument struct {
	end  string
	tabs bool
}

func maskedShellDocuments(text string) string {
	out := []byte(text)
	var pending []shellDocument
	parameter := 0
	for at := 0; at < len(text); at++ {
		switch {
		case text[at] == '\\':
			at++
		case text[at] == '\'' || text[at] == '"' || text[at] == '`':
			at = shellQuoteEnd(text, at) - 1
		case strings.HasPrefix(text[at:], "${"):
			parameter++
			at++
		case parameter > 0:
			if text[at] == '}' {
				parameter--
			}
		case strings.HasPrefix(text[at:], "(("):
			at = shellArithmeticEnd(text, at) - 1
		case text[at] == '#' && (at == 0 || strings.ContainsRune(" \t\r\n;|&()", rune(text[at-1]))):
			for at < len(text) && text[at] != '\n' {
				at++
			}
			at--
		case strings.HasPrefix(text[at:], "<<<"):
			at += 2
		case strings.HasPrefix(text[at:], "<<"):
			doc, end := shellDocumentWord(text, at+2)
			if doc.end != "" {
				pending = append(pending, doc)
				scriptBlank(out, at, end)
				at = end - 1
			}
		case text[at] == '\n' && len(pending) > 0:
			for _, doc := range pending {
				end := shellDocumentEnd(text, at+1, doc)
				scriptBlank(out, at+1, end)
				at = end - 1
			}
			pending = nil
		}
	}
	return string(out)
}

func shellQuoteEnd(text string, at int) int {
	if text[at] == '\'' {
		if end := strings.IndexByte(text[at+1:], '\''); end >= 0 {
			return at + end + 2
		}
		return len(text)
	}
	return scriptQuoteEnd(text, at)
}

func shellDocumentWord(text string, at int) (shellDocument, int) {
	doc := shellDocument{}
	if at < len(text) && text[at] == '-' {
		doc.tabs = true
		at++
	}
	for at < len(text) && strings.ContainsRune(" \t", rune(text[at])) {
		at++
	}
	var word strings.Builder
	quote := byte(0)
	for at < len(text) {
		c := text[at]
		if quote == 0 && strings.ContainsRune(" \t\r\n;|&()<>", rune(c)) {
			break
		}
		if quote != '\'' && c == '\\' && at+1 < len(text) {
			at++
			word.WriteByte(text[at])
		} else if quote == 0 && (c == '\'' || c == '"') {
			quote = c
		} else if c == quote {
			quote = 0
		} else {
			word.WriteByte(c)
		}
		at++
	}
	if quote == 0 {
		doc.end = word.String()
	}
	return doc, at
}

func shellDocumentEnd(text string, at int, doc shellDocument) int {
	for at < len(text) {
		end := strings.IndexByte(text[at:], '\n')
		if end < 0 {
			end = len(text)
		} else {
			end += at
		}
		line := text[at:end]
		if doc.tabs {
			line = strings.TrimLeft(line, "\t")
		}
		at = min(len(text), end+1)
		if line == doc.end {
			return at
		}
	}
	return len(text)
}

func shellArithmeticEnd(text string, at int) int {
	depth := 0
	for ; at < len(text); at++ {
		if text[at] == '(' {
			depth++
		} else if text[at] == ')' {
			depth--
			if depth == 0 {
				return at + 1
			}
		}
	}
	return len(text)
}
