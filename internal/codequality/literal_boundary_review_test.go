package codequality

import (
	"strings"
	"testing"
)

func TestJSXExecutableExpressionsRemainVisibleAfterChildText(t *testing.T) {
	for _, source := range []string{
		"const View = () => <div>Don't {items.map(x => {\n" + strings.Repeat(" work();\n", 81) + "})}</div>;",
		"const View = () => <div title='quoted > attribute'>Text {`template ${() => {\n" +
			strings.Repeat(" work();\n", 81) + "}}`}</div>;",
		"const View = () => <><span>Don't</span>{() => <div>{() => {\n" +
			strings.Repeat(" work();\n", 81) + "}}</div>}</>;",
		"const id = <T,>(x: T) => {\n" + strings.Repeat(" work();\n", 81) + "return x;};",
		"const id = <T extends { id: number }>(x: T) => {\n" + strings.Repeat(" work();\n", 81) + "return x;};",
	} {
		expectRules(t, []File{{"view.tsx", source}}, "function_lines")
	}
	source := "const View = () => <div>Don't class Fake { method() { } }\n" + strings.Repeat(" hello\n", 10) + "</div>;"
	findings, err := Measure([]File{{"view.tsx", source}})
	if err != nil || len(findings) != 0 {
		t.Fatal("JSX child text became executable structure", findings, err)
	}
}

func TestRustLifetimeCommentsAndRawHashesPreserveBoundaries(t *testing.T) {
	for _, literal := range []string{
		`r###""## } "###`, `br" } "`, `cr#"" } "#`, `r` + strings.Repeat("#", 255) + `" } "` + strings.Repeat("#", 255),
	} {
		source := "fn work<'a>() {\n /* nested /* r#\" */ } comment */\n let text = " + literal + ";\n" +
			strings.Repeat(" perform();\n", 81) + "}\n"
		expectRules(t, []File{{"work.rs", source}}, "function_lines")
	}
	source := "fn work() { let s = \"r#\\\" }\"; let c = '}'; }\n"
	findings, err := Measure([]File{{"work.rs", source}})
	if err != nil || len(findings) != 0 {
		t.Fatal("fake raw prefix changed real string boundaries", findings, err)
	}
}
