package main

import (
	"context"
	"strings"
	"testing"

	"coordica/internal/codequality"
)

func TestCompareRefusesNewViolations(t *testing.T) {
	next := []codequality.File{{Path: "src/file.ts", Text: strings.Repeat("x", codequality.MaxLine+1)}}
	if err := compare(context.Background(), nil, next); err == nil {
		t.Fatal("new structural violation passed command gate")
	}
	if err := compare(context.Background(), next, next); err != nil {
		t.Fatal("existing debt blocked incremental adoption", err)
	}
}

func TestCheckRequiresImmutableBaseAndSingleSource(t *testing.T) {
	for _, args := range []struct {
		base, candidate string
		working         bool
	}{
		{"HEAD", "", true},
		{strings.Repeat("a", 40), "", false},
		{strings.Repeat("a", 40), strings.Repeat("b", 40), true},
	} {
		if err := check(context.Background(), ".", args.base, args.candidate, args.working); err == nil {
			t.Fatal("mutable or ambiguous invocation accepted")
		}
	}
}
