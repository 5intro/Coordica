package codequality

import (
	"strings"
	"testing"
)

func TestLanguageScansIgnoreStringsDataAndControlBlocks(t *testing.T) {
	for _, fixture := range []File{
		{"src/work.rs", "fn work<'a>(value: &'a str) -> &'a str { let c = '}'; value }\n"},
		{"src/work.cpp", "int work() {\nif (ready()) {\n" + strings.Repeat("work();\n", 70) + "}\nreturn 0;\n}\n"},
		{"src/work.rb", "class Work\ndef run\nif ready\nitems.each do |item|\nputs item\nend\nend\nend\nend\n"},
		{"src/work.sh", "# function inert() {\nwork() { printf '%s' '}'; }\n"},
		{"src/work.css", ".work {\n" + strings.Repeat("  color: red;\n", MaxFunction) + "}\n"},
		{"src/work.html", "<script type=\"application/json\">\nfunction prose() {\n" + strings.Repeat("text\n", MaxFunction) + "}\n</script>"},
		{"src/work.vue", "<template><div>function prose() {\n" + strings.Repeat("text\n", MaxFunction) + "}</div></template>"},
	} {
		t.Run(fixture.Path, func(t *testing.T) {
			findings, err := Measure([]File{fixture})
			if err != nil || len(findings) != 0 {
				t.Fatal("literal data or native control flow became structural debt", findings, err)
			}
		})
	}
}
