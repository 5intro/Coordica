package codequality

import (
	"strings"
	"testing"
)

func TestPythonContinuationDedentsDoNotCloseDeclaration(t *testing.T) {
	for _, pair := range []struct{ open, close string }{{"[", "]"}, {"(", ")"}, {"{", "}"}} {
		text := "def work():\n    result = " + pair.open + "\n" + strings.Repeat("0,\n", 81) + pair.close + "\n    return result\n"
		if pair.open == "{" {
			text = strings.ReplaceAll(text, "0,\n", "0: 0,\n")
		}
		expectRules(t, []File{{"work.py", text}}, "function_lines")
	}
}

func TestRubyConditionalExpressionDoesNotCloseMethodEarly(t *testing.T) {
	for _, condition := range []string{"value = if true", "value = unless false", "return if true", "puts(if true"} {
		close := "end\n"
		if strings.HasPrefix(condition, "puts(") {
			close = "end)\n"
		}
		source := "def work\n " + condition + "\n  1\n " + close + strings.Repeat(" perform\n", 81) + "end\n"
		// `return if true` is a modifier, not an expression opening a block.
		if condition == "return if true" {
			source = "def work\n return if true\n" + strings.Repeat(" perform\n", 81) + "end\n"
		}
		expectRules(t, []File{{"work.rb", source}}, "function_lines")
	}
}

func TestSwiftInitializersHaveFunctionLimits(t *testing.T) {
	for _, signature := range []string{"init()", "init?()", "init!()", "required init(value: Int)", "convenience init()"} {
		source := "class Worker {\n " + signature + " {\n" + strings.Repeat("  perform()\n", 81) + " }\n}\n"
		expectRules(t, []File{{"work.swift", source}}, "function_lines")
	}
}

func TestCSharpVerbatimStringsDoNotConsumeMethodEnd(t *testing.T) {
	for _, literal := range []string{`@"C:\"`, `@"double ""quoted"" } \"`, `$@"C:\"`, `@$"C:\"`} {
		source := "class Worker { void Work() {\n var path = " + literal + ";\n" + strings.Repeat(" Perform();\n", 81) + "} }\n"
		expectRules(t, []File{{"work.cs", source}}, "function_lines")
	}
}

func TestTypeScriptClassFunctionTypesAreNotExecutableArrows(t *testing.T) {
	for _, property := range []string{"handler!", "handler?", "readonly handler", "static handler"} {
		source := "class Worker {\n " + property + ": () => {\n" + strings.Repeat(" value: number;\n", 81) + " };\n}\n"
		findings, err := Measure([]File{{"work.ts", source}})
		if err != nil || len(findings) != 0 {
			t.Fatal("type-only arrow treated as function", findings, err)
		}
	}
	source := "class Worker {\n handler: () => void = () => {\n" + strings.Repeat(" perform();\n", 81) + " };\n}\n"
	expectRules(t, []File{{"work.ts", source}}, "function_lines")
}

func TestPythonContinuationDoesNotChangeMethodOwnership(t *testing.T) {
	source := "class Worker:\n    def outer(self):\n        value = [\n0,\n]\n"
	source += strings.Repeat("        def inner(): pass\n", 21)
	source += "        return value\n"
	findings, err := Measure([]File{{"work.py", source}})
	if err != nil || len(findings) != 0 {
		t.Fatal("continuation dedent changed method ownership", findings, err)
	}
	source = "def work():\n    value = 1 + \\\n0\n" + strings.Repeat("    perform()\n", 81)
	expectRules(t, []File{{"work.py", source}}, "function_lines")
}

func TestCSharpInterpolationRetainsExecutableCallbacks(t *testing.T) {
	source := "class Worker { void Work() { var text = $@\"value {Create(() => {\n" +
		strings.Repeat(" Perform();\n", 81) + "})}\"; } }\n"
	expectRules(t, []File{{"work.cs", source}}, "function_lines")
}
