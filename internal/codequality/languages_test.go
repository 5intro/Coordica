package codequality

import (
	"fmt"
	"strings"
	"testing"
)

func TestIncludedLanguageFunctionsCannotBypassStructureGate(t *testing.T) {
	body := strings.Repeat("  work();\n", MaxFunction)
	for _, fixture := range []struct{ ext, head, tail string }{
		{"rs", "pub async fn work<T: Send>(value: T) -> Result<(), Error> {\n", "}\n"},
		{"c", "static int work(int value) {\n", "}\n"},
		{"cc", "auto work(int value) -> int {\n", "}\n"},
		{"cpp", "template <typename T> T work(T value) {\n", "}\n"},
		{"h", "inline int work(int value) {\n", "}\n"},
		{"hpp", "inline int work(int value) noexcept {\n", "}\n"},
		{"java", "class Work { static void work() {\n", "}\n}\n"},
		{"cs", "class Work { static void WorkNow() {\n", "}\n}\n"},
		{"kt", "fun work(value: Int): Int {\n", "}\n"},
		{"swift", "func work(value: Int) -> Int {\n", "}\n"},
		{"php", "<?php function work($value): int {\n", "}\n"},
		{"rb", "def work(value)\n", "end\n"},
		{"sh", "work() {\n", "}\n"},
		{"ps1", "function Get-Work {\n", "}\n"},
		{"scss", "@function work($value) {\n", "}\n"},
		{"html", "<script>function work() {\n", "}\n</script>"},
		{"vue", "<script setup lang=\"ts\">const work = () => {\n", "}\n</script>"},
		{"svelte", "<script>function work() {\n", "}\n</script>"},
	} {
		t.Run(fixture.ext, func(t *testing.T) {
			expectRules(t, []File{{"src/work." + fixture.ext, fixture.head + body + fixture.tail}}, "function_lines")
		})
	}
}

func TestNativeClassesCountDirectMethodsAndPhysicalSpan(t *testing.T) {
	for _, fixture := range []struct{ ext, head, method, tail string }{
		{"cpp", "class Work {\n", "void run%d() {}\n", "};"},
		{"php", "<?php class Work {\n", "public function run%d() {}\n", "}"},
		{"rs", "impl Work {\n", "fn run%d(&self) {}\n", "}"},
		{"rb", "class Work\n", "def run%d()\nend\n", "end"},
		{"ps1", "class Work {\n", "[void] Run%d() {}\n", "}"},
	} {
		t.Run(fixture.ext, func(t *testing.T) {
			var source strings.Builder
			source.WriteString(fixture.head)
			for i := 0; i <= MaxMethods; i++ {
				fmt.Fprintf(&source, fixture.method, i)
			}
			source.WriteString(strings.Repeat("\n", MaxClass) + fixture.tail)
			expectRules(t, []File{{"src/work." + fixture.ext, source.String()}}, "class_methods", "class_lines")
		})
	}
}
