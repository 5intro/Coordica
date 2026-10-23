package codequality

import (
	"fmt"
	"strings"
	"testing"
)

func TestNativeOperatorClosureAndExpressionMethods(t *testing.T) {
	for _, fixture := range []struct{ path, open, close string }{
		{"work.cpp", "bool Item::operator==(const Item& other) {\n", "}\n"},
		{"work.cpp", "Item Item::operator+(const Item& other) {\n", "}\n"},
		{"work.rs", "fn main() { let work = |value| {\n", "}; }\n"},
		{"work.rs", "fn main() { let work = move || {\n", "}; }\n"},
	} {
		expectRules(t, []File{{fixture.path, fixture.open + strings.Repeat(" work();\n", 81) + fixture.close}}, "function_lines")
	}
	expectRules(t, []File{{"work.cs", "class Worker { Result Work() => Build(\n" +
		strings.Repeat(" 1,\n", 81) + " 1); }\n"}}, "function_lines")
}

func TestCppRawStringsCannotCloseAFunctionEarly(t *testing.T) {
	for _, literal := range []string{`R"( " } )"`, `u8R"tag( " } )tag"`, `LR"custom( " } )custom"`} {
		source := "void work() {\n auto value = " + literal + ";\n" + strings.Repeat(" run();\n", 81) + "}\n"
		expectRules(t, []File{{"work.cpp", source}}, "function_lines")
	}
}

func TestCSharpPartialMethodsAggregateByQualifiedType(t *testing.T) {
	part := func(namespace, name string, start, count int) string {
		source := "namespace " + namespace + ";\npartial class " + name + " {\n"
		for i := start; i < start+count; i++ {
			source += fmt.Sprintf(" void Work%d() {}\n", i)
		}
		return source + "}\n"
	}
	base := []File{{"first.cs", part("App", "Worker", 0, 11)}, {"second.cs", part("App", "Worker", 11, 10)}}
	expectRules(t, base, "class_methods")
	base[1].Text = part("Other", "Worker", 11, 10)
	findings, err := Measure(base)
	if err != nil || len(findings) != 0 {
		t.Fatal("unrelated types shared method ownership", findings, err)
	}
	base = []File{{"first.cs", part("App", "Worker", 0, 12)}, {"second.cs", part("App", "Worker", 12, 11)}}
	candidate := []File{{"second.cs", part("App", "Worker", 0, 21)}}
	findings, err = Regressions(base, candidate)
	if err != nil || len(findings) != 0 {
		t.Fatal("partial type cleanup lost its original allowance", findings, err)
	}
	for index := range base {
		base[index].Text = "namespace App { partial class Outer<T> { partial class Worker {\n" +
			strings.Repeat(" int field;\n", 110) + "} } }\n"
	}
	expectRules(t, base, "class_lines")
}
