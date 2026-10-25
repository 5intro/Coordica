package codequality

import (
	"regexp"
	"strings"
)

var rustLifetime = regexp.MustCompile(`'[A-Za-z_][A-Za-z_0-9]*`)

func nativeMaskedSource(text, extension string) string {
	if extension == ".cs" {
		return maskedCSharp(text)
	}
	if extension == ".php" {
		return maskedHashComments(text, false)
	}
	if strings.Contains("|.cc|.cpp|.h|.hpp|", "|"+extension+"|") {
		text = maskedCppRawStrings(text)
	}
	if extension != ".rs" {
		return maskedSource(text, false, nil)
	}
	text = maskedRustRawStrings(text)
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
