package codequality

import (
	"fmt"
	"strings"
	"testing"
)

func TestExpressionBodiedArrowsMeasureAuthoredExtent(t *testing.T) {
	for _, expression := range []string{
		"(\n" + strings.Repeat("  value +\n", MaxFunction) + "  value\n)",
		"call(\n" + strings.Repeat("  value,\n", MaxFunction) + "  value\n)",
		"<main>\n" + strings.Repeat("  <span/>\n", MaxFunction) + "</main>",
		"<>\n" + strings.Repeat("  <span title={value}/>\n", MaxFunction) + "</>",
		"`\n" + strings.Repeat("  template text\n", MaxFunction) + "`",
		"value\n" + strings.Repeat("  + value\n", MaxFunction),
	} {
		t.Run(strings.Split(expression, "\n")[0], func(t *testing.T) {
			expectRules(t, []File{{"src/view.tsx", "const render = () => " + expression + ";\n"}}, "function_lines")
		})
	}
}

func TestArrowParameterExtentAndBlockBodyAreMeasured(t *testing.T) {
	var parameters strings.Builder
	for i := 0; i < MaxFunction; i++ {
		fmt.Fprintf(&parameters, "  value%d = 1,\n", i)
	}
	text := "const run = (\n" + parameters.String() + ") => value0;\n"
	expectRules(t, []File{{"src/view.ts", text}}, "function_lines")
	text = "const run = ({value}) => {\n" + strings.Repeat("  work();\n", MaxFunction) + "};\n"
	expectRules(t, []File{{"src/view.ts", text}}, "function_lines")
}

func TestArrowExpressionStopsBeforeOtherStatementsAndComments(t *testing.T) {
	for _, expression := range []string{
		"const first = () => value;\n", "const first = () => value\n", "const first = () => <span/>;\n",
		"const first = () => /[{}]/;\n", "const first = () => ({value: 1});\n",
	} {
		text := expression + strings.Repeat("// unrelated module comment\n", MaxFunction) + "const second = () => value;\n"
		findings, err := Measure([]File{{"src/view.tsx", text}})
		if err != nil || len(findings) != 0 {
			t.Fatal("unrelated statements extended the expression-bodied function", expression, findings, err)
		}
	}
}
