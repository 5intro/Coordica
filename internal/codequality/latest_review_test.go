package codequality

import (
	"fmt"
	"strings"
	"testing"
)

func TestRubyLiteralKeywordsCannotCloseAuthoredBlocks(t *testing.T) {
	for _, literal := range []string{`/end/`, `%r{end}`, `%q{do end class}`, `%w[end do class]`, `:end`} {
		t.Run(literal, func(t *testing.T) {
			source := "def work\n  value = " + literal + "\n" + strings.Repeat("  work_item\n", 82) + "end\n"
			expectRules(t, []File{{"work.rb", source}}, "function_lines")
		})
	}
	findings, err := Measure([]File{{"work.rb", "def work\n  ratio = total / count\nend\n"}})
	if err != nil || len(findings) != 0 {
		t.Fatal("Ruby division became a function", findings, err)
	}
}

func TestAnonymousJavaScriptClassesCannotEvadeClassLimits(t *testing.T) {
	for _, header := range []string{"const Service = class", "export default class", "const Service = class extends Base"} {
		source := header + " {\n" + strings.Repeat("  field = 1;\n", 201) + "}\n"
		expectRules(t, []File{{"work.ts", source}}, "class_lines")
		source = header + " {\n"
		for i := 0; i < 21; i++ {
			source += fmt.Sprintf("  method%d() {}\n", i)
		}
		expectRules(t, []File{{"work.ts", source + "}\n"}}, "class_methods")
	}
}

func TestAnonymousClassDebtCannotMoveToAnotherBinding(t *testing.T) {
	class := func(name string, lines int) string {
		return "const " + name + " = class {\n" + strings.Repeat("  field = 1;\n", lines) + "}\n"
	}
	base := []File{{"work.js", class("small", 201) + class("large", 210)}}
	findings, err := Regressions(base, []File{{"work.js", class("small", 210)}})
	if err != nil || len(findings) == 0 {
		t.Fatal("anonymous class borrowed another class's allowance", findings, err)
	}
}

func TestFunctionTypeBodiesAreNotExecutableArrows(t *testing.T) {
	properties := strings.Repeat("  field: string;\n", 82)
	for _, source := range []string{
		"type Handler = () => {\n" + properties + "};\n",
		"type Handler<T> =\n  () => {\n" + properties + "};\n",
		"interface Props { handler: () => {\n" + properties + "}; }\n",
		"const handler: () => {\n" + properties + "} = factory;\n",
	} {
		findings, err := Measure([]File{{"work.ts", source}})
		if err != nil || len(findings) != 0 {
			t.Fatal("type-only syntax measured as implementation", findings, err)
		}
	}
	body := strings.Repeat("  perform();\n", 82)
	for _, source := range []string{
		"const handler: () => void = () => {\n" + body + "};\n",
		"const object = {handler: () => {\n" + body + "}};\n",
	} {
		expectRules(t, []File{{"work.ts", source}}, "function_lines")
	}
}

func TestMultilinePythonLambdasAndBindingDebt(t *testing.T) {
	lambda := func(name string, lines int) string {
		return name + " = lambda value: (\n" + strings.Repeat("    value +\n", lines) + "    0\n)\n"
	}
	expectRules(t, []File{{"work.py", lambda("handler", 81)}}, "function_lines")
	base := []File{{"work.py", lambda("small", 81) + lambda("large", 90)}}
	findings, err := Regressions(base, []File{{"work.py", lambda("small", 90)}})
	if err != nil || len(findings) == 0 {
		t.Fatal("lambda inherited another binding's debt", findings, err)
	}
}

func TestParameterlessScssAndShellTrimCannotEvadeFunctionLimits(t *testing.T) {
	expectRules(t, []File{{"work.scss", "@mixin reset {\n" + strings.Repeat("  color: red;\n", 82) + "}\n"}}, "function_lines")
	for _, expression := range []string{"${value#prefix}", "${value##prefix}", "${value%tail}", "${value/#x/y}"} {
		source := "work() {\n  value=" + expression + "\n" + strings.Repeat("  echo value\n", 82) + "}\n"
		expectRules(t, []File{{"work.sh", source}}, "function_lines")
	}
}
