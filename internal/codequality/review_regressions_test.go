package codequality

import (
	"fmt"
	"strings"
	"testing"
)

func TestJavaScriptRegexDoesNotCloseFunctionOrClass(t *testing.T) {
	for _, expression := range []string{
		`const re = /}/;`, `const re = /[{}'"/]/g;`, `const re = /\//;`,
		`if (ready) /}/.test(value);`, `return /}/;`, `const re = /https?:\/\/[^}]+/;`,
	} {
		t.Run(expression, func(t *testing.T) {
			text := "function work() {\n" + expression + "\n" + strings.Repeat("work();\n", MaxFunction) + "}\n"
			expectRules(t, []File{{"src/work.ts", text}}, "function_lines")
			text = "class Work {\nrun() { const re = /}/; }\n" + strings.Repeat("\n", MaxClass) + "}\n"
			expectRules(t, []File{{"src/work.js", text}}, "class_lines")
		})
	}
}

func TestJavaScriptDivisionDoesNotMaskFollowingBlocks(t *testing.T) {
	for _, expression := range []string{
		`const value = total / count / scale;`, `const value = f() / 2;`,
		`const value = items[0] / 2;`, `const value = total / /* comment */ count;`,
	} {
		text := expression + "\nfunction work() {\n" + strings.Repeat("work();\n", MaxFunction) + "}\n"
		expectRules(t, []File{{"src/work.ts", text}}, "function_lines")
	}
}

func TestReceiverDebtSurvivesRemovalOfRepresentativeFile(t *testing.T) {
	var base []File
	for i := 0; i < MaxMethods+2; i++ {
		base = append(base, File{fmt.Sprintf("src/m%02d.go", i), fmt.Sprintf("package p\nfunc (s *Service) M%d() {}\n", i)})
	}
	findings, err := Regressions(base, base[1:])
	if err != nil || len(findings) != 0 {
		t.Fatal("decreasing receiver debt rejected after removing its first file", findings, err)
	}
	renamed := append([]File(nil), base[1:]...)
	for i := range renamed {
		renamed[i].Text = strings.ReplaceAll(renamed[i].Text, "*Service", "*Renamed")
	}
	findings, err = Regressions(base, renamed)
	if err != nil || len(findings) == 0 {
		t.Fatal("receiver rename inherited debt", findings, err)
	}
}

func TestStructuralDispatchUsesNormalizedExtension(t *testing.T) {
	for _, extension := range []string{"GO", "TS", "JS", "PY"} {
		text := "function work() {\n" + strings.Repeat("work();\n", MaxFunction) + "}\n"
		if extension == "GO" {
			text = "package p\nfunc work() {\n" + strings.Repeat("println(1)\n", MaxFunction) + "}\n"
		} else if extension == "PY" {
			text = "def work():\n" + strings.Repeat("    pass\n", MaxFunction)
		}
		expectRules(t, []File{{"src/work." + extension, text}}, "function_lines")
	}
}

func TestPythonTrailingModuleContentDoesNotExtendFunction(t *testing.T) {
	for _, suffix := range []string{
		strings.Repeat("# module comment\n", MaxFunction),
		"\"\"\"module documentation\n" + strings.Repeat("notes\n", MaxFunction) + "\"\"\"\n",
	} {
		findings, err := Measure([]File{{"src/work.py", "def work():\n    pass\n\n" + suffix}})
		if err != nil || len(findings) != 0 {
			t.Fatal("module content counted as function body", findings, err)
		}
	}
}

func TestPythonOwnedCommentsAndMultilineStringsStillCount(t *testing.T) {
	for _, body := range []string{
		strings.Repeat("    # function comment\n", MaxFunction) + "    pass\n",
		"    \"\"\"documentation\n" + strings.Repeat("unindented string text\n", MaxFunction) + "    \"\"\"\n",
		"    pass\n# intermediate comment\n" + strings.Repeat("    pass\n", MaxFunction),
	} {
		expectRules(t, []File{{"src/work.py", "def work():\n" + body}}, "function_lines")
	}
}
