package codequality

import (
	"fmt"
	"strings"
	"testing"
)

func expectRules(t *testing.T, files []File, rules ...string) {
	t.Helper()
	findings, err := Measure(files)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, f := range findings {
		seen[f.Rule] = true
	}
	for _, rule := range rules {
		if !seen[rule] {
			t.Fatalf("missing %s: %+v", rule, findings)
		}
	}
}

func TestGoStructureAndCrossFileReceiver(t *testing.T) {
	files := []File{{Path: "src/type.go", Text: "package p\ntype Service[T any] struct{}\n"}}
	for i := 0; i < MaxMethods+1; i++ {
		files = append(files, File{
			Path: fmt.Sprintf("src/m%02d.go", i),
			Text: fmt.Sprintf("package p\nfunc (s *Service[T]) M%d() {}\n", i),
		})
	}
	expectRules(t, files, "receiver_methods")
	long := "package p\nfunc Work() {\n" + strings.Repeat("println(1)\n", MaxFunction) + "}\n"
	expectRules(t, []File{{Path: "src/long.go", Text: long}}, "function_lines")
}

func TestTextLimitsUseCharactersAndCRLF(t *testing.T) {
	text := strings.Repeat("中", MaxLine+1) + "\r\n" + strings.Repeat("x\n", MaxFile)
	expectRules(t, []File{{Path: "src/test.ts", Text: text}}, "line_length", "file_lines")
	findings, err := Measure([]File{{Path: "src/test.ts", Text: strings.Repeat("中", MaxLine) + "\r\n"}})
	if err != nil || len(findings) != 0 {
		t.Fatal("UTF-8 bytes or CR counted as characters", findings, err)
	}
}

func TestDebtFreezeRejectsGrowthRenameAndDuplicate(t *testing.T) {
	line := strings.Repeat("x", MaxLine+1)
	base := []File{{Path: "src/test.js", Text: line + "\n"}}
	cases := []struct {
		name string
		file File
		fail bool
	}{
		{"unchanged moved line", File{"src/test.js", "\n" + line + "\n"}, false},
		{"fixed", File{"src/test.js", "ok\n"}, false},
		{"duplicate", File{"src/test.js", line + "\n" + line + "\n"}, true},
		{"longer", File{"src/test.js", line + "x\n"}, true},
		{"renamed", File{"src/renamed.js", line + "\n"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			findings, err := Regressions(base, []File{tc.file})
			if err != nil || (len(findings) > 0) != tc.fail {
				t.Fatal(findings, err)
			}
		})
	}
}

func TestMeasureFailsClosedOnMalformedOrUnboundedSource(t *testing.T) {
	for _, files := range [][]File{
		{{"src/bad.go", "package"}},
		{{"src/bad.ts", "\xff"}},
		{{"src/bad.ts", "a\x00b"}},
		{{"src/huge.ts", strings.Repeat("x", MaxFileBytes+1)}},
		{{"src/dup.ts", "a"}, {"src/dup.ts", "b"}},
	} {
		if _, err := Measure(files); err == nil {
			t.Fatal("invalid inventory passed")
		}
	}
}

func TestGeneratedClaimsDoNotExemptAuthoredCode(t *testing.T) {
	text := "// Code generated; DO NOT EDIT\n" + strings.Repeat("x", MaxLine+1)
	expectRules(t, []File{{Path: "src/user.ts", Text: text}}, "line_length")
	for _, name := range []string{"web/dist/output.js", "vendor/package/code.go", "web/src/api.generated.ts"} {
		if Included(name) {
			t.Fatal("fixed artifact convention included", name)
		}
	}
}

func TestNativeAndLexicalStructures(t *testing.T) {
	methods := ""
	for i := 0; i < MaxMethods+1; i++ {
		methods += fmt.Sprintf("m%d() { return '}'; }\n", i)
	}
	expectRules(t, []File{{"src/class.ts", "class Service {\n" + methods + "}\n"}}, "class_methods")
	for _, prefix := range []string{"function work() {\n", "const work = () => {\n"} {
		expectRules(t, []File{{"src/function.ts", prefix + strings.Repeat("work();\n", MaxFunction) + "}\n"}}, "function_lines")
	}
	py := "class Service:\n"
	for i := 0; i < MaxMethods+1; i++ {
		py += fmt.Sprintf("    def m%d(self):\n        pass\n", i)
	}
	expectRules(t, []File{{"src/service.py", py}}, "class_methods")
	expectRules(t, []File{{"src/service.py", "def work():\n" + strings.Repeat("    pass\n", MaxFunction)}}, "function_lines")
}

func TestMultilinePythonSignatureAndNestedFunctions(t *testing.T) {
	py := "def work(\n    value,\n):\n" + strings.Repeat("    pass\n", MaxFunction)
	expectRules(t, []File{{"src/work.py", py}}, "function_lines")
	py = "class Small:\n    def work(self):\n"
	for i := 0; i < MaxMethods+1; i++ {
		py += fmt.Sprintf("        def nested%d():\n            pass\n", i)
	}
	findings, err := Measure([]File{{"src/work.py", py}})
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range findings {
		if finding.Rule == "class_methods" {
			t.Fatal("nested local functions counted as class methods", finding)
		}
	}
}
