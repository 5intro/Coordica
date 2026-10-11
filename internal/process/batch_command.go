package process

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

// ErrUnsafeBatchCommand means an installed Windows batch shim cannot be
// represented by our deliberately restricted invocation. Use a supported
// installation path or a native executable; do not escape into a shell string.
var ErrUnsafeBatchCommand = errors.New("unsupported Windows batch command")

const maxBatchCommandUnits = 8191

func isBatchShim(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".cmd") || strings.HasSuffix(lower, ".bat")
}

// buildBatchCommand is platform independent so the restrictions and exact
// command construction can be tested even when no Windows host is available.
// Windows uses it only for a resolved installed .cmd/.bat file, never model text.
func buildBatchCommand(interpreter, shim string, args []string) (string, error) {
	for _, p := range []string{interpreter, shim} {
		if !absoluteWindowsDrivePath(p) || unsafeBatchValue(p) || strings.ContainsAny(p[2:], ":*?") {
			return "", fmt.Errorf("%w: installation path contains unsupported characters or is not a local absolute drive path", ErrUnsafeBatchCommand)
		}
	}
	if !strings.EqualFold(interpreter[strings.LastIndexAny(interpreter, `\/`)+1:], "cmd.exe") || !isBatchShim(shim) {
		return "", fmt.Errorf("%w: expected system cmd.exe and a .cmd/.bat shim", ErrUnsafeBatchCommand)
	}
	var command strings.Builder
	command.WriteString(`"` + interpreter + `" /d /s /v:off /c ""` + shim + `"`)
	for _, arg := range args {
		// A quoted trailing backslash changes quote decoding when an npm shim
		// forwards %* to node.exe. Reject it rather than guess the next parser.
		if unsafeBatchValue(arg) || strings.HasSuffix(arg, `\`) {
			return "", fmt.Errorf("%w: argument contains unsupported characters or a trailing backslash", ErrUnsafeBatchCommand)
		}
		command.WriteString(` "` + arg + `"`)
	}
	command.WriteByte('"')
	line := command.String()
	if len(utf16.Encode([]rune(line))) > maxBatchCommandUnits {
		return "", fmt.Errorf("%w: command exceeds the Windows cmd limit; use stdin for input", ErrUnsafeBatchCommand)
	}
	return line, nil
}

func absoluteWindowsDrivePath(p string) bool {
	return len(p) >= 3 && ((p[0] >= 'A' && p[0] <= 'Z') || (p[0] >= 'a' && p[0] <= 'z')) && p[1] == ':' && (p[2] == '\\' || p[2] == '/')
}

func unsafeBatchValue(s string) bool {
	if !utf8.ValidString(s) || strings.ContainsAny(s, `"%!^&|<>()`) {
		return true
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}
