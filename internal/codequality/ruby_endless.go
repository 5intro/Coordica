package codequality

import "strings"

func rubyEndlessEnd(m *metrics, f File, text string, at int, ends map[int]int) (int, bool) {
	at = scriptSpace(text, at)
	name := rubyName.FindString(text[at:])
	if name == "" {
		return -1, false
	}
	at = scriptSpace(text, at+len(name))
	if at < len(text) && text[at] == '(' {
		close, ok := ends[at]
		if !ok {
			return -1, false
		}
		at = scriptSpace(text, close+1)
	}
	if at >= len(text) || text[at] != '=' || strings.HasPrefix(text[at:], "==") || strings.HasPrefix(text[at:], "=>") {
		return -1, false
	}
	body := scriptSpace(text, at+1)
	end := scriptExpressionEnd(f.Text, text, body, ends)
	if end < body {
		return max(at, end), true
	}
	for _, token := range rubyToken.FindAllStringIndex(text[body:end+1], -1) {
		word := text[body+token[0] : body+token[1]]
		if word == "begin" || word == "if" || word == "unless" || word == "case" || word == "do" {
			if close := rubyExpressionBlockEnd(m, text, body+token[0]); close >= 0 {
				end = max(end, close)
			}
			break
		}
	}
	return end, true
}

func rubyExpressionBlockEnd(m *metrics, text string, start int) int {
	depth, previousLine, loop := 0, -1, false
	for _, token := range rubyToken.FindAllStringIndex(text[start:], -1) {
		if m.cancelled() {
			return -1
		}
		at, end := start+token[0], start+token[1]
		word := text[at:end]
		line := m.lineAt(at)
		if previousLine != line {
			loop = false
			previousLine = line
		}
		begin := max(start, strings.LastIndexByte(text[:at], '\n')+1)
		prefix := strings.TrimSpace(text[begin:at])
		if word == "end" {
			depth--
			if depth == 0 {
				return end - 1
			}
			continue
		}
		if (word == "if" || word == "unless" || word == "while" || word == "until") && prefix != "" && !strings.HasSuffix(prefix, ";") {
			continue
		}
		if word == "do" && loop {
			continue
		}
		loop = word == "while" || word == "until" || word == "for"
		depth++
	}
	return -1
}
