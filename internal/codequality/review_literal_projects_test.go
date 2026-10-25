package codequality

import (
	"fmt"
	"strings"
	"testing"
)

func TestJSXChildQuotesCannotHideFunctionBodies(t *testing.T) {
	for _, open := range []string{
		`<div>Don't hide the body`, `<><div>Don't</div>`,
		`<div title="quoted > value">Don't {true && <span>hide</span>}`,
	} {
		close := "</div>"
		if strings.HasPrefix(open, "<>") {
			close = "</>"
		}
		source := "export function View() { return (" + open + "\n" + strings.Repeat(" body text\n", 81) + close + "); }\n"
		for _, name := range []string{"view.jsx", "view.tsx"} {
			expectRules(t, []File{{name, source}}, "function_lines")
		}
	}
	source := "const View = () => <button onClick={() => {\n" + strings.Repeat(" work();\n", 81) + "}}>Don't</button>;\n"
	expectRules(t, []File{{"view.tsx", source}}, "function_lines")
}

func TestRustRawStringsDoNotExposeLiteralBraces(t *testing.T) {
	for _, literal := range []string{`r#"" } "#`, `r##""# } "##`, `br#"" } "#`, `r" } "`} {
		source := "fn work() {\n let text = " + literal + ";\n" + strings.Repeat(" perform();\n", 81) + "}\n"
		expectRules(t, []File{{"work.rs", source}}, "function_lines")
	}
}

func csharpProjectPart(prefix string, count int) string {
	s := "namespace Shared; partial class Worker {\n"
	for i := 0; i < count; i++ {
		s += fmt.Sprintf(" void %s%d() {}\n", prefix, i)
	}
	return s + "}\n"
}

func TestCSharpPartialCountsUseCompilationProjectScopes(t *testing.T) {
	const project = `<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><TargetFramework>net10.0</TargetFramework></PropertyGroup></Project>`
	files := []File{{"one/one.csproj", project}, {"two/two.csproj", project},
		{"one/part.cs", csharpProjectPart("One", 11)}, {"two/part.cs", csharpProjectPart("Two", 11)}}
	findings, err := Measure(files)
	if err != nil || len(findings) != 0 {
		t.Fatal("separate assemblies shared a partial-type allowance", findings, err)
	}
	files = []File{{"app.csproj", project}, {"one/part.cs", csharpProjectPart("One", 11)},
		{"two/part.cs", csharpProjectPart("Two", 11)}}
	expectRules(t, files, "class_methods")
	// A nested project does not remove its source from the ancestor SDK's default glob.
	files = append(files, File{"two/nested.csproj", project})
	expectRules(t, files, "class_methods")
}

func TestRubyReceiverKeywordCallsCannotHideMethodBodies(t *testing.T) {
	for _, call := range []string{"value.class", "value&.class", "value.class()", "value.public_send(:class)"} {
		source := "def work(value)\n " + call + "\n" + strings.Repeat(" perform\n", 81) + "end\n"
		expectRules(t, []File{{"work.rb", source}}, "function_lines")
	}
}
