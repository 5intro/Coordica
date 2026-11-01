package domain

import (
	"strings"
	"testing"
)

func TestCheckPathContractsAreBoundedAndDoNotGrantSourceAuthority(t *testing.T) {
	for _, name := range []string{"/absolute", "../escape", "out/../source", "C:/out", "out\\file", ".git/objects/**", "out//file", "out/["} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateCheckPaths(Check{OutputPaths: []string{name}}); err == nil {
				t.Fatal("unsafe output declaration accepted", name)
			}
		})
	}
	for _, check := range []Check{
		{RequiredPaths: []string{"src/**"}}, {RequiredPaths: []string{"manifest", "manifest"}},
		{OutputPaths: strings.Split(strings.Repeat("x,", 129), ",")},
	} {
		if err := ValidateCheckPaths(check); err == nil {
			t.Fatal("invalid input/output contract accepted", check)
		}
	}
	for _, check := range []Check{{}, {RequiredPaths: []string{"package-lock.json", "src/入口.ts"}, OutputPaths: []string{"build/**", "cache/**"}}} {
		if err := ValidateCheckPaths(check); err != nil {
			t.Fatal("valid/legacy check rejected", err)
		}
	}
}
