package codequality

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestDenseNativeMemberOwnershipCompletesWithinBound(t *testing.T) {
	var source strings.Builder
	for i := 0; i < 2000; i++ {
		fmt.Fprintf(&source, "class C%d { void work() {} } ", i)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	findings, err := MeasureContext(ctx, []File{{"dense.java", source.String()}})
	if err != nil || !hasRule(findings, "line_length") {
		t.Fatal("bounded native input could not return its deterministic regression", err, findings)
	}
}

func TestNativeAttributesDoNotBecomeMethods(t *testing.T) {
	for _, fixture := range []struct{ path, field string }{
		{"fields.java", `@Column(name="x") int value%d;` + "\n"},
		{"fields.java", `@db.Column(name="x") int value%d;` + "\n"},
		{"fields.cs", `[Column(Name="x")] public int Value%d;` + "\n"},
		{"fields.cs", `[Column(Choose("x"))] public int Value%d;` + "\n"},
	} {
		var source strings.Builder
		source.WriteString("class Fields {\n")
		for i := 0; i < 21; i++ {
			fmt.Fprintf(&source, fixture.field, i)
		}
		source.WriteString("}\n")
		findings, err := Measure([]File{{fixture.path, source.String()}})
		if err != nil || hasRule(findings, "class_methods") {
			t.Fatal("annotation/attribute calls became declarations", fixture.path, err, findings)
		}
	}
}

func TestKotlinNewlineDeclarationsAndExpressionsAreMeasured(t *testing.T) {
	var declarations strings.Builder
	declarations.WriteString("interface Worker {\n")
	for i := 0; i < 21; i++ {
		fmt.Fprintf(&declarations, " fun work%d(): String\n", i)
	}
	expectRules(t, []File{{"work.kt", declarations.String() + "}\n"}}, "class_methods")
	for _, body := range []struct{ begin, line, end string }{
		{"listOf(\n", " %d,\n", ")\n"},
		{"when (0) {\n", " %d -> 0\n", " else -> 0\n}\n"},
		{"object {\n", " val field%d = 0\n", "}\n"},
	} {
		source := "fun work() = " + body.begin
		for i := 0; i < 81; i++ {
			source += fmt.Sprintf(body.line, i)
		}
		source += body.end
		expectRules(t, []File{{"work.kt", source}}, "function_lines")
	}
}

func TestRustImplPartsAggregateByModuleAndType(t *testing.T) {
	impl := func(header string, first, count int) string {
		text := header + " {\n"
		for i := first; i < first+count; i++ {
			text += fmt.Sprintf(" fn work%d(&self) {}\n", i)
		}
		return text + "}\n"
	}
	for _, files := range [][]File{
		{{"src/lib.rs", "struct Worker;\n" + impl("impl Worker", 0, 11) + impl("impl Worker", 11, 10)}},
		{{"src/lib.rs", "mod extra;\nstruct Worker;\n" + impl("impl Worker", 0, 11)},
			{"src/extra.rs", "use crate::Worker;\n" + impl("impl Worker", 11, 10)}},
		{{"src/lib.rs", "mod extra;\nstruct Worker;\n" + impl("impl Worker", 0, 11)},
			{"src/extra.rs", impl("impl crate::Worker", 11, 10)}},
		{{"src/lib.rs", "struct Worker<T>(T);\n" + impl("impl<T> Worker<T>", 0, 11) + impl("impl<U> Worker<U>", 11, 10)}},
		{{"src/lib.rs", "mod extra;\nstruct Worker;\n" + impl("impl Worker", 0, 11)},
			{"src/extra.rs", "use crate::{Worker as Alias};\n" + impl("impl Alias", 11, 10)}},
		{{"src/lib.rs", "mod extra;\nstruct Worker;\n" + impl("impl Worker", 0, 11)},
			{"src/extra.rs", "use super::Worker;\n" + impl("impl Worker", 11, 10)}},
	} {
		expectRules(t, files, "class_methods")
	}
	files := []File{{"src/a.rs", "struct Worker;\n" + impl("impl Worker", 0, 11)},
		{"src/b.rs", "struct Worker;\n" + impl("impl Worker", 11, 11)}}
	findings, err := Measure(files)
	if err != nil || hasRule(findings, "class_methods") {
		t.Fatal("unrelated Rust modules were aggregated", err, findings)
	}
}

func TestRustNamespacesAndDebtStayStableAcrossImplFiles(t *testing.T) {
	makeImpl := func(first, count int) string {
		var source strings.Builder
		source.WriteString("impl Worker {\n")
		for i := first; i < first+count; i++ {
			fmt.Fprintf(&source, "fn work%d(&self) {}\n", i)
		}
		return source.String() + "}\n"
	}
	base := []File{{"src/lib.rs", "mod extra;\nstruct Worker;\n" + makeImpl(0, 22)}, {"src/extra.rs", "use crate::Worker;\n"}}
	next := []File{{"src/lib.rs", "mod extra;\nstruct Worker;\n"}, {"src/extra.rs", "use crate::Worker;\n" + makeImpl(0, 21)}}
	findings, err := Regressions(base, next)
	if err != nil || len(findings) != 0 {
		t.Fatal("shrinking impl debt changed identity after moving representative file", findings, err)
	}
	source := "mod a { struct Worker;\n" + makeImpl(0, 11) + "}\nmod b { struct Worker;\n" + makeImpl(0, 11) + "}\n"
	findings, err = Measure([]File{{"src/lib.rs", source}})
	if err != nil || hasRule(findings, "class_methods") {
		t.Fatal("inline module types were combined", findings, err)
	}
}

func TestJavaEnumsHaveClassAndMethodLimits(t *testing.T) {
	source := "enum Worker { ONE, TWO;\n"
	for i := 0; i < 21; i++ {
		source += fmt.Sprintf(" void work%d() {}\n", i)
	}
	expectRules(t, []File{{"work.java", source + "}\n"}}, "class_methods")
	expectRules(t, []File{{"work.java", "enum Worker { ONE;\n" + strings.Repeat("\n", 201) + "}\n"}}, "class_lines")
}

func hasRule(findings []Finding, rule string) bool {
	for _, finding := range findings {
		if finding.Rule == rule {
			return true
		}
	}
	return false
}
