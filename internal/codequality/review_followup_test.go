package codequality

import (
	"fmt"
	"strings"
	"testing"
)

func TestCppLambdasHaveBoundedBodiesAndScopedDebt(t *testing.T) {
	lambda := func(name string, lines int) string {
		return "auto " + name + " = [value](int x) mutable noexcept {\n" + strings.Repeat("work();\n", lines) + "};\n"
	}
	expectRules(t, []File{{"work.cpp", lambda("small", 81)}}, "function_lines")
	base := []File{{"work.cpp", lambda("small", 81) + lambda("large", 91)}}
	findings, err := Regressions(base, []File{{"work.cpp", lambda("small", 91)}})
	if err != nil || len(findings) == 0 {
		t.Fatal("lambda inherited another binding's debt", findings, err)
	}
	for _, header := range []string{"[]()", "[]", "[&] noexcept", "[]<typename T>(T x)"} {
		expectRules(t, []File{{"work.cpp", "auto fn = " + header + " {\n" + strings.Repeat("work();\n", 81) + "};"}}, "function_lines")
	}
	for _, fixture := range []File{
		{"work.cs", "var fn = () => {\n" + strings.Repeat("Work();\n", 81) + "};"},
		{"work.java", "var fn = () -> {\n" + strings.Repeat("work();\n", 81) + "};"},
	} {
		expectRules(t, []File{fixture}, "function_lines")
	}
}

func TestComputedCallableFieldsCountAsClassMethods(t *testing.T) {
	for _, value := range []string{"() => value", "function() { return value; }", "async () => value"} {
		source := "class Worker {\n"
		for i := 0; i < 21; i++ {
			source += fmt.Sprintf(" [handlers.key%d] = %s;\n", i, value)
		}
		expectRules(t, []File{{"work.ts", source + "}\n"}}, "class_methods")
	}
}

func TestPhysicalLineMetricsRecognizeCRWithoutDoublingCRLF(t *testing.T) {
	for _, newline := range []string{"\r", "\r\n", "\n"} {
		function := "function work() {" + newline + strings.Repeat(newline, 81) + "}"
		expectRules(t, []File{{"work.js", function}}, "function_lines")
		expectRules(t, []File{{"work.css", strings.Repeat(newline, 501)}}, "file_lines")
		findings, err := Measure([]File{{"short.js", "function short() {" + newline + "}"}})
		if err != nil || len(findings) != 0 {
			t.Fatal("short CR/CRLF source miscounted", findings, err)
		}
	}
}

func TestShellHeredocTextCannotCloseAuthoredFunction(t *testing.T) {
	for _, declaration := range []string{"<<EOF", "<<'EOF'", "<<\"EOF\"", "<<-EOF"} {
		source := "work() {\ncat " + declaration + "\n}\nEOF\n" + strings.Repeat("echo x\n", 81) + "}\n"
		expectRules(t, []File{{"work.sh", source}}, "function_lines")
	}
	for _, command := range []string{
		"cat <<-EOF\n\t}\n\tEOF\n", "cat <<EOF <<OTHER\n}\nEOF\n}\nOTHER\n",
		"cat <<E\"O\"F\n}\nEOF\n", "cat <<\\EOF\n}\nEOF\n",
		"value=$((1<<2))\n", "cat <<< 'text'\n", "echo 'literal <<EOF'\n",
	} {
		expectRules(t, []File{{"work.sh", "work() {\n" + command + strings.Repeat("echo x\n", 81) + "}\n"}}, "function_lines")
	}
}

func TestJavaScriptClassHeadersBalanceExtendsExpressions(t *testing.T) {
	for _, header := range []string{"class Worker extends mixin({mode: true})", "const Worker = class extends mixin({})"} {
		source := header + " {\n" + strings.Repeat("field = 1;\n", 201) + "}\n"
		expectRules(t, []File{{"work.ts", source}}, "class_lines")
		source = header + " {\n"
		for i := 0; i < 21; i++ {
			source += fmt.Sprintf("method%d() {}\n", i)
		}
		expectRules(t, []File{{"work.ts", source + "}\n"}}, "class_methods")
	}
}
