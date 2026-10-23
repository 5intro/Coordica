package codequality

import (
	"fmt"
	"strings"
	"testing"
)

func TestCallableClassFieldsShareMethodLimit(t *testing.T) {
	for _, initializer := range []string{
		"() => 1", "value => value", "async value => value", "async () => 1",
		"function() { return 1; }", "<T>(value: T): T => value", "() => { return () => 1; }",
	} {
		var source strings.Builder
		source.WriteString("class Work {\nrun() {}\n")
		for i := 0; i < MaxMethods; i++ {
			fmt.Fprintf(&source, "field%d = %s;\n", i, initializer)
		}
		source.WriteString("}\n")
		expectRules(t, []File{{"src/work.ts", source.String()}}, "class_methods")
	}
}

func TestClassFieldTypesAndNestedCallbacksDoNotInflateMethodCount(t *testing.T) {
	var source strings.Builder
	source.WriteString("class Work {\nrun() {}\nhandler: (value: number) => number = (value) => value;\n")
	for i := 0; i < MaxMethods+1; i++ {
		fmt.Fprintf(&source, "value%d = factory(() => 1);\n", i)
	}
	source.WriteString("method() { const nested = function() {}; return nested; }\n}\n")
	findings, err := Measure([]File{{"src/work.ts", source.String()}})
	if err != nil || len(findings) != 0 {
		t.Fatal("nested implementation or callable type inflated class count", findings, err)
	}
}
