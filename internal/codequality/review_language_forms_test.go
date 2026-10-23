package codequality

import (
	"fmt"
	"strings"
	"testing"
)

func TestPowerShellModulesAreAuthoredSource(t *testing.T) {
	for _, extension := range []string{"psm1", "PSM1"} {
		expectRules(t, []File{{"module." + extension, strings.Repeat("\n", 501)}}, "file_lines")
		expectRules(t, []File{{"module." + extension, "function Invoke-Work {\n" + strings.Repeat(" Write-Output value\n", 81) + "}\n"}},
			"function_lines")
	}
}

func TestTypeScriptBodylessMethodsCountWithoutNestedSignatures(t *testing.T) {
	for _, header := range []string{"abstract class Worker {\n", "declare class Worker {\n"} {
		source := header
		modifier := ""
		if strings.HasPrefix(header, "abstract") {
			modifier = "abstract "
		}
		for i := 0; i < 21; i++ {
			source += fmt.Sprintf(" %swork%d(): void;\n", modifier, i)
		}
		expectRules(t, []File{{"work.ts", source + "}\n"}}, "class_methods")
	}
	decorated := "class Worker {\n"
	for i := 0; i < 21; i++ {
		decorated += fmt.Sprintf(" @Tag(1)\n work%d(): void {}\n", i)
	}
	expectRules(t, []File{{"decorated.ts", decorated + "}\n"}}, "class_methods")
	fields := "class Fields {\n"
	for i := 0; i < 21; i++ {
		fields += fmt.Sprintf(" @Tag(1)\n value%d: number;\n", i)
	}
	findings, err := Measure([]File{{"fields.ts", fields + "}\n"}})
	if err != nil || hasRule(findings, "class_methods") {
		t.Fatal("decorators became bodyless methods", findings, err)
	}
	source := "class Worker {\n work() {\n"
	for i := 0; i < 21; i++ {
		source += fmt.Sprintf(" type Signature%d = { call(): void };\n", i)
	}
	source += " }\n}\n"
	findings, err = Measure([]File{{"work.ts", source}})
	if err != nil || hasRule(findings, "class_methods") {
		t.Fatal("nested type signatures counted as methods", findings, err)
	}
}

func TestParenthesizedGoReceiversRemainDistinct(t *testing.T) {
	source := "package sample\ntype A struct{}\ntype B struct{}\n"
	for _, name := range []string{"A", "B"} {
		for i := 0; i < 11; i++ {
			source += fmt.Sprintf("func (value (%s)) Work%d() {}\n", name, i)
		}
	}
	findings, err := Measure([]File{{"work.go", source}})
	if err != nil || hasRule(findings, "receiver_methods") {
		t.Fatal("parenthesized receivers lost type identity", findings, err)
	}
	for i := 11; i < 21; i++ {
		source += fmt.Sprintf("func (value (*A)) Work%d() {}\n", i)
	}
	expectRules(t, []File{{"work.go", source}}, "receiver_methods")
}

func TestRubyBraceBlocksAreFunctionsButHashesAreNot(t *testing.T) {
	for _, header := range []string{"items.each { |item|\n", "handler = ->(item) {\n", "handler = proc {\n"} {
		expectRules(t, []File{{"work.rb", header + strings.Repeat(" work_item\n", 81) + "}\n"}}, "function_lines")
	}
	for _, hash := range []struct{ start, end string }{{"values = {\n", "}\n"}, {"call({\n", "})\n"}} {
		findings, err := Measure([]File{{"work.rb", hash.start + strings.Repeat(" value: 1,\n", 81) + hash.end}})
		if err != nil || hasRule(findings, "function_lines") {
			t.Fatal("hash literal measured as closure", findings, err)
		}
	}
}
