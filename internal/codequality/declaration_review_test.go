package codequality

import (
	"fmt"
	"strings"
	"testing"
)

func TestDeclarationOnlyNativeMethodsCount(t *testing.T) {
	for _, fixture := range []struct{ path, header, method string }{
		{"work.java", "interface Worker {\n", " void work%d();\n"},
		{"work.cs", "interface Worker {\n", " void Work%d();\n"},
		{"work.rs", "trait Worker {\n", " fn work%d(&self);\n"},
	} {
		source := fixture.header
		for i := 0; i < 21; i++ {
			source += fmt.Sprintf(fixture.method, i)
		}
		expectRules(t, []File{{fixture.path, source + "}\n"}}, "class_methods")
	}
}

func TestNativeOverloadDebtCannotMoveBetweenSignatures(t *testing.T) {
	for _, extension := range []string{"java", "cs", "cpp"} {
		method := func(arg string, lines int) string {
			return "void work(" + arg + ") {\n" + strings.Repeat("perform();\n", lines) + "}\n"
		}
		base := []File{{"work." + extension, "class Worker {\n" + method("int x", 81) + method("double x", 91) + "}\n"}}
		candidate := []File{{"work." + extension, "class Worker {\n" + method("int x", 91) + "}\n"}}
		findings, err := Regressions(base, candidate)
		if err != nil || len(findings) == 0 {
			t.Fatal("overload borrowed another signature's allowance", extension, findings, err)
		}
	}
}

func TestShellCompoundFunctionBodiesCannotEvadeLength(t *testing.T) {
	for _, body := range []struct{ start, end string }{
		{"(\n", ")\n"}, {"while true; do\n", "done\n"}, {"if true; then\n", "fi\n"},
		{"for x in a; do\n", "done\n"}, {"case x in\na)\n", ";;\nesac\n"},
	} {
		source := "work() " + body.start + strings.Repeat(" echo value\n", 81) + body.end
		expectRules(t, []File{{"work.sh", source}}, "function_lines")
	}
}

func TestPythonConditionalClassMethodsAreCountedButNestedFunctionsAreNot(t *testing.T) {
	source := "class Worker:\n    if True:\n"
	for i := 0; i < 21; i++ {
		source += fmt.Sprintf("        def work%d(self):\n            pass\n", i)
	}
	expectRules(t, []File{{"work.py", source}}, "class_methods")
	source = "class Worker:\n    def work(self):\n"
	for i := 0; i < 21; i++ {
		source += fmt.Sprintf("        def nested%d():\n            pass\n", i)
	}
	findings, err := Measure([]File{{"work.py", source}})
	if err != nil || len(findings) != 0 {
		t.Fatal("nested functions were counted as class methods", findings, err)
	}
}

func TestRubyEndlessDefinitionsDoNotConsumeTheClassEnd(t *testing.T) {
	source := "class Worker\n"
	for i := 0; i < 21; i++ {
		source += fmt.Sprintf(" def work%d = value\n", i)
	}
	expectRules(t, []File{{"work.rb", source + "end\n"}}, "class_methods")
	source = "class Worker\n def work = value\n" + strings.Repeat(" field = 1\n", 201) + "end\n"
	expectRules(t, []File{{"work.rb", source}}, "class_lines")
	for _, expression := range []struct{ open, close string }{{"(\n", ")"}, {"begin\n", "end"}} {
		source = "def work = " + expression.open + strings.Repeat(" value;\n", 81) + expression.close
		expectRules(t, []File{{"work.rb", source}}, "function_lines")
	}
}

func TestNativeRecordsAreClassLikeDeclarations(t *testing.T) {
	for _, fixture := range []struct{ path, header string }{
		{"work.java", "record Worker(int x)"}, {"work.cs", "record Worker(int x)"}, {"work.cs", "record struct Worker(int x)"},
	} {
		source := fixture.header + " {\n"
		for i := 0; i < 21; i++ {
			source += fmt.Sprintf("void work%d() {}\n", i)
		}
		expectRules(t, []File{{fixture.path, source + "}\n"}}, "class_methods")
		expectRules(t, []File{{fixture.path, fixture.header + " {\n" + strings.Repeat("\n", 201) + "}\n"}}, "class_lines")
	}
}
