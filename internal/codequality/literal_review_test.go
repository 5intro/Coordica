package codequality

import (
	"fmt"
	"strings"
	"testing"
)

func TestOnlyActualScriptTypeAttributesCanHideExecutableCode(t *testing.T) {
	for _, attrs := range []string{`data-type="application/json"`, `data-note=' type="application/json" '`,
		`type="module" type="application/json"`, `data-note='>' data-type="application/json"`} {
		source := "<script " + attrs + ">function work() {\n" + strings.Repeat("run();\n", 81) + "}</script>"
		expectRules(t, []File{{"work.html", source}}, "function_lines")
	}
}

func TestShellQuotedCommandSubstitutionsRemainExecutable(t *testing.T) {
	for _, open := range []string{"value=\"$(work() {\n", "value=\"text $(printf '%s' \"$(work() {\n"} {
		close := "}; work)\"\n"
		if strings.Contains(open, "printf") {
			close = "}; work)\")\"\n"
		}
		expectRules(t, []File{{"work.sh", open + strings.Repeat(" echo value\n", 81) + close}}, "function_lines")
	}
	for _, open := range []string{"value='$(work() {\n", "value=\"\\$(work() {\n"} {
		close := "}; work)\"\n"
		if strings.HasPrefix(open, "value='") {
			close = "}; work)'\n"
		}
		findings, err := Measure([]File{{"work.sh", open + strings.Repeat(" echo value\n", 81) + close}})
		if err != nil || len(findings) != 0 {
			t.Fatal("literal shell content counted as code", findings, err)
		}
	}
}

func TestPythonClassLambdaBindingsCountAsMethods(t *testing.T) {
	for _, prefix := range []string{"class Worker:\n", "class Worker:\n    if True:\n"} {
		indent := "    "
		if strings.Contains(prefix, "if True") {
			indent += "    "
		}
		source := prefix
		for i := 0; i < 21; i++ {
			source += fmt.Sprintf("%swork%d = lambda self: 1\n", indent, i)
		}
		expectRules(t, []File{{"work.py", source}}, "class_methods")
	}
	source := "class Worker:\n"
	for i := 0; i < 21; i++ {
		source += fmt.Sprintf("    work%d = (\n        lambda self: 1\n    )\n", i)
	}
	expectRules(t, []File{{"work.py", source}}, "class_methods")
	source = "class Worker:\n    def work(self):\n"
	for i := 0; i < 21; i++ {
		source += fmt.Sprintf("        work%d = lambda: 1\n", i)
	}
	findings, err := Measure([]File{{"work.py", source}})
	if err != nil || len(findings) != 0 {
		t.Fatal("method-local lambda values were counted as class members", findings, err)
	}
}
