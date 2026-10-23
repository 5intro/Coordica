package codequality

import (
	"fmt"
	"strings"
	"testing"
)

func TestJavaScriptBalancedFunctionAndMethodSignatures(t *testing.T) {
	body := strings.Repeat("  work();\n", MaxFunction)
	for _, source := range []string{
		"function work<T>() {\n" + body + "}\n",
		"function work<T extends {value: number}>({value}: T) {\n" + body + "}\n",
		"function work({value} = {value: 1}, [first] = [1]) {\n" + body + "}\n",
		"function work<T>({value}: {value: T}): {value: T} {\n" + body + "}\n",
		"class Work {\nrun({value}) {\n" + body + "}\n}\n",
		"class Work {\nrun<T extends {value: number}>({value}: T): Promise<{value: T}> {\n" + body + "}\n}\n",
		"class Work {\nrun({value}): {value: number} & {ready: boolean} {\n" + body + "}\n}\n",
		"function work(): (value: number) => {value: number} {\n" + body + "}\n",
	} {
		t.Run(strings.Split(source, "\n")[0], func(t *testing.T) {
			expectRules(t, []File{{Path: "src/work.ts", Text: source}}, "function_lines")
		})
	}
}

func TestDestructuringMethodsAndCallableFieldsCountWithoutNestedCalls(t *testing.T) {
	var methods, fields strings.Builder
	for i := 0; i <= MaxMethods; i++ {
		fmt.Fprintf(&methods, "run%d({value}, [first]) { return call({value}, first); }\n", i)
		fmt.Fprintf(&fields, "field%d = function work({value}) { return value; };\n", i)
	}
	expectRules(t, []File{{"src/work.ts", "class Work {\n" + methods.String() + "}\n"}}, "class_methods")
	expectRules(t, []File{{"src/work.ts", "class Work {\n" + fields.String() + "}\n"}}, "class_methods")
}

func TestTypeOverloadsAndShortDestructuringDeclarationsRemainWithinLimits(t *testing.T) {
	source := `function work<T>(value: T): {value: T};
function work<T>(value: T): {value: T} { return {value}; }
class Work {
  run({value}: {value: number}): {value: number} { return {value}; }
  runAll<T extends {value: number}>({value}: T) { return value; }
}`
	findings, err := Measure([]File{{"src/work.ts", source}})
	if err != nil || len(findings) != 0 {
		t.Fatal("balanced short signature created false structural debt", findings, err)
	}
}
