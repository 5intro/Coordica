package codequality

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestAnalysisHonorsCancellationInsideOneLargeSource(t *testing.T) {
	fixture := File{"large.js", strings.Repeat("function f(){}\n", 100000)}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()
	started := time.Now()
	_, err := RegressionsContext(ctx, nil, []File{fixture})
	var defect *CandidateError
	if !errors.Is(err, context.Canceled) || errors.As(err, &defect) {
		t.Fatal("cancellation became candidate defect or success", err)
	}
	if time.Since(started) > 2*time.Second {
		t.Fatal("analysis did not stop promptly")
	}
}

func TestAnalysisPrecancelAndLineIndex(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := MeasureContext(ctx, []File{{"a.js", "function a(){}"}}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	text := "a\n\nb\n"
	lines := sourceLineOffsets(text)
	for offset := 0; offset <= len(text); offset++ {
		if got, want := indexedLine(lines, offset), strings.Count(text[:offset], "\n")+1; got != want {
			t.Fatalf("offset %d: got %d want %d", offset, got, want)
		}
	}
}
