package codequality

import (
	"context"
	"strings"
)

// Callable fields own class behavior just as prototype methods do. Skip
// complete initializers and nested bodies so their local callbacks are not
// mistaken for additional members of the enclosing class.
func scriptCallableFields(ctx context.Context, original, text string, open, end int, ends map[int]int) int {
	count := 0
	for at := open + 1; at < end; {
		if ctx != nil && ctx.Err() != nil {
			return count
		}
		if close, ok := ends[at]; ok && text[at] == '[' {
			at = close + 1
		} else if close, ok := ends[at]; ok {
			at = close + 1
			continue
		} else if !scriptWord(text[at]) {
			at++
			continue
		} else {
			for at < end && scriptWord(text[at]) {
				at++
			}
		}
		assignment := scriptFieldAssignment(text, at, end, ends)
		if assignment < 0 {
			continue
		}
		start := scriptSpace(text, assignment+1)
		if scriptCallableValue(text, start, ends) {
			count++
		}
		at = max(start, scriptExpressionEnd(original, text, start, ends)+1)
	}
	return count
}

func scriptFieldAssignment(text string, at, end int, ends map[int]int) int {
	at = scriptSpace(text, at)
	if at < end && strings.ContainsRune("!?", rune(text[at])) {
		at = scriptSpace(text, at+1)
	}
	if at < end && text[at] == ':' {
		for at++; at < end; at++ {
			if close, ok := ends[at]; ok {
				at = close
			} else if text[at] == ';' {
				return -1
			} else if text[at] == '=' && !strings.HasPrefix(text[at:], "=>") {
				break
			}
		}
	}
	if at < end && text[at] == '=' && !strings.HasPrefix(text[at:], "=>") {
		return at
	}
	return -1
}

func scriptCallableValue(text string, at int, ends map[int]int) bool {
	if strings.HasPrefix(text[at:], "async ") {
		at = scriptSpace(text, at+6)
	}
	if match := scriptFunction.FindStringIndex(text[at:]); match != nil && match[0] == 0 {
		return true
	}
	if at < len(text) && text[at] == '<' {
		at = scriptTypeArguments(text, at, ends)
		if at < 0 {
			return false
		}
		at = scriptSpace(text, at)
	}
	if close, ok := ends[at]; ok && text[at] == '(' {
		at = scriptSpace(text, close+1)
		if at < len(text) && text[at] == ':' {
			// A return annotation follows complete parameters, not a call.
			at += strings.Index(text[at:], "=>")
		}
	} else {
		for at < len(text) && scriptWord(text[at]) {
			at++
		}
		at = scriptSpace(text, at)
	}
	return at >= 0 && at < len(text) && strings.HasPrefix(text[at:], "=>")
}
