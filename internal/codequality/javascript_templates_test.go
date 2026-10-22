package codequality

import (
	"strings"
	"testing"
)

func TestTemplateInterpolationsRetainExecutableFunctions(t *testing.T) {
	long := "() => {\n" + strings.Repeat("work();\n", MaxFunction) + "}"
	for _, source := range []string{
		"const text = `value ${(" + long + ")()}`;",
		"const text = `outer ${`inner ${(" + long + ")()}`} tail`;",
		"const text = `value ${({run: " + long + "}).run()}`;",
		"const text = `value ${/}/.test(value) ? (" + long + ")() : 0}`;",
	} {
		expectRules(t, []File{{"src/work.js", source}}, "function_lines")
	}
}

func TestTemplateLiteralTextAndEscapedInterpolationAreInert(t *testing.T) {
	long := "() => {\n" + strings.Repeat("work();\n", MaxFunction) + "}"
	for _, source := range []string{
		"const text = `" + long + "`;",
		"const text = `\\${" + long + "}`;",
		"const text = `outer ${`inner ${42}`} tail`;",
	} {
		findings, err := Measure([]File{{"src/work.js", source}})
		if err != nil || len(findings) != 0 {
			t.Fatal("literal prose became executable source", findings, err)
		}
	}
	// The containing arrow still owns the entire multiline template, including
	// an inner template that must not prematurely close its expression span.
	source := "const work = () => `outer ${`inner ${42}`}\n" + strings.Repeat("text\n", MaxFunction) + "`;"
	expectRules(t, []File{{"src/work.js", source}}, "function_lines")
}
