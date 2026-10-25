package codequality

import (
	"fmt"
	"strings"
	"testing"
)

func TestQuotedJavaScriptMethodsCannotBypassStructureLimits(t *testing.T) {
	for _, name := range []string{`"work"`, `'work'`, `"work with space"`, `"work\\\"quoted"`} {
		source := "class Worker { " + name + "() {\n" + strings.Repeat(" perform();\n", 81) + "} }\n"
		expectRules(t, []File{{"worker.ts", source}}, "function_lines")
		source = "const worker = { " + name + "() {\n" + strings.Repeat(" perform();\n", 81) + "} };\n"
		expectRules(t, []File{{"worker.ts", source}}, "function_lines")
	}
	var source strings.Builder
	source.WriteString("class Worker {\n")
	for i := 0; i < 21; i++ {
		fmt.Fprintf(&source, " \"work %d\"() {}\n", i)
	}
	source.WriteString("}\n")
	expectRules(t, []File{{"worker.ts", source.String()}}, "class_methods")
}

func TestQuotedMethodDebtRetainsItsPropertyIdentity(t *testing.T) {
	body := "() {\n" + strings.Repeat(" perform();\n", 81) + "} }\n"
	base := []File{{"worker.ts", "class Worker { \"original\"" + body}}
	candidate := []File{{"worker.ts", "class Worker { \"replacement\"" + body}}
	findings, err := Regressions(base, candidate)
	if err != nil || len(findings) != 1 || findings[0].Rule != "function_lines" {
		t.Fatal(findings, err)
	}
	source := "class Worker {\n // \"fake\"() {\n value = '\"fake\"() {';\n run() {}\n}\n"
	findings, err = Measure([]File{{"worker.ts", source}})
	if err != nil || len(findings) != 0 {
		t.Fatal("literal/comment became a method", findings, err)
	}
}
