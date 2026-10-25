package codequality

import (
	"fmt"
	"strings"
	"testing"
)

func TestNativeClassDebtCannotMoveAcrossNamespacesOrEnclosingTypes(t *testing.T) {
	class := func(scope string, lines int) string {
		return "namespace " + scope + " { class Shared {\n" + strings.Repeat(" int value;\n", lines) + "} }\n"
	}
	base := []File{{"work.cs", class("A", 201) + class("B", 1)}}
	next := []File{{"work.cs", class("A", 1) + class("B", 201)}}
	findings, err := Regressions(base, next)
	if err != nil || !hasRule(findings, "class_lines") {
		t.Fatal("class borrowed another namespace's allowance", findings, err)
	}
}

func TestMarkupCommentsAndAttributeTextAreNotExecutableScript(t *testing.T) {
	body := "function old() {\n" + strings.Repeat(" perform();\n", 81) + "}\n"
	for _, extension := range []string{"html", "vue", "svelte"} {
		for _, source := range []string{"<!-- <script>" + body + "</script> -->", `<div title="<script>` + body + `</script>">Text</div>`} {
			findings, err := Measure([]File{{"page." + extension, source}})
			if err != nil || hasRule(findings, "function_lines") {
				t.Fatal("inert markup counted as executable", extension, findings, err)
			}
		}
		expectRules(t, []File{{"page." + extension, "<!-- hidden <script></script> -->\n<script>" + body + "</script>"}}, "function_lines")
	}
}

func TestNativeExpressionLambdasAndDelegateTypes(t *testing.T) {
	for _, fixture := range []struct{ path, arrow, target string }{
		{"work.cs", "=>", "System.Func<int,int>"}, {"Work.java", "->", "java.util.function.Function<Integer,Integer>"},
	} {
		t.Run(fixture.path, func(t *testing.T) {
			source := "class Worker { void Run() { " + fixture.target + " handler = value " + fixture.arrow + " Build(\n" +
				strings.Repeat(" value,\n", 81) + " value); } }\n"
			findings, err := Measure([]File{{fixture.path, source}})
			found := false
			for _, finding := range findings {
				found = found || finding.Rule == "function_lines" && finding.Subject == "lambda"
			}
			if err != nil || !found {
				t.Fatal("expression lambda itself was not measured", findings, err)
			}
		})
	}
	source := "class Worker {\n"
	for i := 0; i < 21; i++ {
		source += fmt.Sprintf(" public delegate void Handler%d(int value);\n", i)
	}
	findings, err := Measure([]File{{"work.cs", source + "}\n"}})
	if err != nil || hasRule(findings, "class_methods") {
		t.Fatal("nested delegate types became methods", findings, err)
	}
}

func TestAnonymousJavaClassesAndTypeScriptInterfacesAreClassLike(t *testing.T) {
	for _, fixture := range []struct{ path, open, method, close string }{
		{"Work.java", "class Factory { Object value = new Object() {\n", " void work%d() {}\n", "}; }\n"},
		{"work.ts", "interface Worker {\n", " work%d(): void;\n", "}\n"},
	} {
		t.Run(fixture.path, func(t *testing.T) {
			source := fixture.open
			for i := 0; i < 21; i++ {
				source += fmt.Sprintf(fixture.method, i)
			}
			expectRules(t, []File{{fixture.path, source + fixture.close}}, "class_methods")
			findings, err := Measure([]File{{fixture.path, fixture.open + strings.Repeat("\n", 201) + fixture.close}})
			found := false
			for _, finding := range findings {
				found = found || finding.Rule == "class_lines" &&
					(fixture.path != "Work.java" || strings.HasPrefix(finding.Subject, "anonymous"))
			}
			if err != nil || !found {
				t.Fatal("class-like body not measured", findings, err)
			}
		})
	}
}
