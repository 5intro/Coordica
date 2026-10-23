package codequality

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestInvalidBaselineCanBeCleanedWithoutTransferringDebt(t *testing.T) {
	for _, invalid := range []string{"package p\nfunc broken(\n", string([]byte{0xff, 0x00})} {
		base := []File{{"broken.go", invalid}, {"debt.ts", strings.Repeat("x", MaxLine+1)}}
		for _, clean := range [][]File{base[1:], {{"broken.go", "package p\n"}, base[1]}} {
			findings, err := Regressions(base, clean)
			if err != nil || len(findings) != 0 {
				t.Fatal("fixed/deleted baseline source is permanently blocked", findings, err)
			}
		}
		findings, err := Regressions(base, []File{{"broken.go", "package p\nfunc work() {\n" + strings.Repeat("println(1)\n", 81) + "}\n"}})
		if err != nil || len(findings) == 0 {
			t.Fatal("unmeasurable baseline granted new function debt", findings, err)
		}
		_, err = Regressions(base, base)
		var candidate *CandidateError
		if !errors.As(err, &candidate) {
			t.Fatal("unchanged invalid candidate passed", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := RegressionsContext(ctx, []File{{"broken.go", "bad"}}, nil); !errors.Is(err, context.Canceled) {
		t.Fatal("baseline cancellation became repairable debt", err)
	}
}

func TestComputedJavaScriptMethodsCannotEvadeLimits(t *testing.T) {
	for _, name := range []string{"[Symbol.iterator]", "['run']", "[getKey()]", "[keys[names[0]]]"} {
		for _, header := range []string{"class Worker {\n", "const object = {\n"} {
			source := header + name + "() {\n" + strings.Repeat("work();\n", 81) + "}\n};\n"
			expectRules(t, []File{{"work.ts", source}}, "function_lines")
		}
	}
	source := "class Worker {\n"
	for i := 0; i < 21; i++ {
		source += fmt.Sprintf("  ['run%d']() {}\n", i)
	}
	expectRules(t, []File{{"work.ts", source + "}\n"}}, "class_methods")
}

func TestPythonFunctionDebtIsScopedToEnclosingDeclarations(t *testing.T) {
	method := func(class string, lines int) string {
		return "class " + class + ":\n    def work(self):\n" + strings.Repeat("        pass\n", lines)
	}
	base := []File{{"work.py", method("A", 81) + method("B", 91)}}
	findings, err := Regressions(base, []File{{"work.py", method("A", 91)}})
	if err != nil || len(findings) == 0 {
		t.Fatal("method borrowed another class's debt", findings, err)
	}
	findings, err = Regressions(base, []File{{"work.py", method("A", 80)}})
	if err != nil || len(findings) != 0 {
		t.Fatal("valid shrinking method rejected", findings, err)
	}
}

func TestPowerShellAndPHPCommentsCannotCloseFunctions(t *testing.T) {
	for _, fixture := range []struct{ path, header, comment string }{
		{"work.ps1", "function Work {\n", "<#\n}\n#>\n"},
		{"work.php", "<?php\nfunction work() {\n", "# }\n"},
	} {
		t.Run(fixture.path, func(t *testing.T) {
			source := fixture.header + fixture.comment + strings.Repeat("work();\n", 81) + "}\n"
			expectRules(t, []File{{fixture.path, source}}, "function_lines")
		})
	}
}

func TestCppConstructorInitializersAreNotExtraMethods(t *testing.T) {
	for _, initializer := range []string{"member(make(x))", "member{make(x)}", "Base(x), member{make(x)}"} {
		source := "class Worker {\npublic:\n"
		for i := 0; i < 11; i++ {
			source += fmt.Sprintf("  Worker(Type%d x) : %s {}\n", i, initializer)
		}
		findings, err := Measure([]File{{"work.cpp", source + "};\n"}})
		if err != nil || len(findings) != 0 {
			t.Fatal("initializer calls were counted as constructor methods", findings, err)
		}
		body := "class Worker {\n Worker() : " + initializer + " {\n" + strings.Repeat("work();\n", 81) + "}\n};\n"
		expectRules(t, []File{{"work.cpp", body}}, "function_lines")
	}
}
