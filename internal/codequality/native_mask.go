package codequality

import "regexp"

var rustLifetime = regexp.MustCompile(`'[A-Za-z_][A-Za-z_0-9]*`)

func nativeMaskedSource(text, extension string) string {
	if extension != ".rs" {
		return maskedSource(text, false, nil)
	}
	// Rust lifetimes are not character literals. Mask just their apostrophe
	// before generic quote handling; preserve real quoted character literals.
	out := []byte(text)
	for _, match := range rustLifetime.FindAllStringIndex(text, -1) {
		if match[1] == len(text) || text[match[1]] != '\'' {
			out[match[0]] = ' '
		}
	}
	return maskedSource(string(out), false, nil)
}
