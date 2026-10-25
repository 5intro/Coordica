package codequality

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestGoVariantAnalysisBudgetFailsClosedWithoutOperationalTimeout(t *testing.T) {
	files := []File{{"worker.go", "package sample\ntype Worker struct{}\n"}}
	for i := 0; i < 20; i++ {
		for j := i + 1; j < 20; j++ {
			condition := fmt.Sprintf("//go:build (tag%d || tag%d) && !(tag%d && tag%d)\n\n", i, j, i, j)
			files = append(files, File{fmt.Sprintf("part_%d_%d.go", i, j), receiverSource(condition, fmt.Sprintf("Work%d_%d_", i, j), 1)})
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := MeasureContext(ctx, files)
	var source *SourceError
	if !errors.As(err, &source) || errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("complex source retried as operational timeout", err)
	}
	findings, err := RegressionsContext(ctx, files, files)
	if err != nil || len(findings) != 0 {
		t.Fatal("unchanged conditional debt required exponential reanalysis", findings, err)
	}
}
