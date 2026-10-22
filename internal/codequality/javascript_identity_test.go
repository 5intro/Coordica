package codequality

import (
	"strings"
	"testing"
)

func TestJavaScriptObjectMethodsCannotBypassFunctionLimit(t *testing.T) {
	for _, header := range []string{"work()", "work({value})", "async work<T>({value}: T)", "*work()", "get work()"} {
		source := "const service = {\n" + header + " {\n" + strings.Repeat("  work();\n", MaxFunction) + "}\n};\n"
		expectRules(t, []File{{"src/service.ts", source}}, "function_lines")
	}
	short := "const service = { work() { if (ready) { return call(); } } };"
	findings, err := Measure([]File{{"src/service.js", short}})
	if err != nil || len(findings) != 0 {
		t.Fatal("control blocks were counted as object methods", findings, err)
	}
}

func TestDeletingArrowCannotDonateItsDebtToAnotherBinding(t *testing.T) {
	arrow := func(name string, lines int) string {
		return "const " + name + " = () => {\n" + strings.Repeat("  work();\n", lines-2) + "};\n"
	}
	base := []File{{"src/work.js", arrow("small", 90) + arrow("large", 100)}}
	for _, changed := range []string{arrow("small", 100), arrow("small", 101)} {
		findings, err := Regressions(base, []File{{"src/work.js", changed}})
		if err != nil || len(findings) != 1 || findings[0].Value < 100 {
			t.Fatal("deleted arrow allowance hid growth in a different binding", findings, err)
		}
	}
	findings, err := Regressions(base, []File{{"src/work.js", "\n" + arrow("small", 85)}})
	if err != nil || len(findings) != 0 {
		t.Fatal("shrinking the surviving arrow or shifting lines lost its identity", findings, err)
	}
}

func TestAnonymousCallbackDebtIncludesItsCallSiteAndEnclosingOwner(t *testing.T) {
	callback := func(label string, lines int) string {
		return "it('" + label + "', () => {\n" + strings.Repeat("  work();\n", lines-2) + "});\n"
	}
	base := []File{{"src/work.js", callback("first", 90) + callback("second", 100)}}
	findings, err := Regressions(base, []File{{"src/work.js", callback("first", 100)}})
	if err != nil || len(findings) == 0 {
		t.Fatal("separate call sites shared anonymous function debt", findings, err)
	}
}
