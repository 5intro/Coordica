package codequality

import (
	"strings"
	"testing"
)

func TestGoFunctionLiteralsIncludeBindingsCallbacksAndNestedClosures(t *testing.T) {
	body := strings.Repeat("println(1)\n", MaxFunction)
	for _, source := range []string{
		"var work = func() {\n" + body + "}\n",
		"func work() { run(\"case\", func() {\n" + body + "}) }\n",
		"var work = func() { returnValue := func() {\n" + body + "}; _ = returnValue }\n",
	} {
		expectRules(t, []File{{"src/work.go", "package p\n" + source}}, "function_lines")
	}
}

func TestGoLiteralDebtBindsToDeclarationAndCallSite(t *testing.T) {
	long := "func() {\n" + strings.Repeat("println(1)\n", MaxFunction+2) + "}"
	shorter := "func() {\n" + strings.Repeat("println(1)\n", MaxFunction+1) + "}"
	base := []File{{"src/work.go", "package p\nvar first = " + long + "\nvar second = func(){}\n"}}
	allowed := []File{{"src/work.go", "package p\n\nvar first = " + shorter + "\nvar second = func(){}\n"}}
	violations, err := Regressions(base, allowed)
	if err != nil || len(violations) != 0 {
		t.Fatal("line shifts or decreasing literal debt rejected", violations, err)
	}
	moved := []File{{"src/work.go", "package p\nvar first = func(){}\nvar second = " + long + "\n"}}
	violations, err = Regressions(base, moved)
	if err != nil || len(violations) == 0 {
		t.Fatal("literal transferred debt to another declaration", violations, err)
	}
	call := func(label string) []File {
		return []File{{"src/work.go", "package p\nfunc work(){ run(" + label + ", " + long + ") }\n"}}
	}
	violations, err = Regressions(call(`"first"`), call(`"second"`))
	if err != nil || len(violations) == 0 {
		t.Fatal("callback transferred debt to another call site", violations, err)
	}
}
