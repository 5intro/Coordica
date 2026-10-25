package codequality

import "strings"

func rubyPostfixModifier(prefix string) bool {
	if prefix == "" {
		return false
	}
	// An unfinished expression expects a block-valued conditional; a complete
	// receiver/value followed by if/unless/while/until is a statement modifier.
	if strings.ContainsRune(";=([{,:?+-*/%&|^!<>", rune(prefix[len(prefix)-1])) {
		return false
	}
	words := strings.Fields(prefix)
	switch words[len(words)-1] {
	case "then", "else", "do", "begin":
		return false
	}
	return true
}
