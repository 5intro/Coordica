package app

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"coordica/internal/store"
)

func TestCheckPlanStoreReadFailureCanRetryOriginalCandidate(t *testing.T) {
	f := newITFixture(t)
	f.implement(t, "src/feature.txt")
	before := f.snapshot(t)
	candidate := f.candidate(t)
	ctx := context.Background()
	if err := f.s.DB.Update(ctx, func(tx *store.Tx) error {
		return tx.Put("test_suite", "unreadable", map[string]any{"checks": "invalid stored check list"})
	}); err != nil {
		t.Fatal(err)
	}
	_, failure := candidateCheckPlan(ctx, f.w, *before.Project, &candidate)
	var quality *qualityScanFailure
	if failure == nil || errors.As(failure, &quality) {
		t.Fatal("did not reach the suite read after successful quality analysis", failure)
	}
	if err := candidateCheckFailure(ctx, f.w, candidate, failure); !errors.Is(err, failure) {
		t.Fatal("store read failure was not preserved", err)
	}
	if err := f.s.DB.Update(ctx, func(tx *store.Tx) error { return tx.Delete("test_suite", "unreadable") }); err != nil {
		t.Fatal(err)
	}
	after := f.snapshot(t)
	if !reflect.DeepEqual(before.Candidates, after.Candidates) || !reflect.DeepEqual(before.Tasks, after.Tasks) {
		t.Fatal("infrastructure failure consumed repair authority")
	}
	if _, err := candidateCheckPlan(ctx, f.w, *after.Project, &candidate); err != nil {
		t.Fatal("original fixed candidate could not retry", err)
	}
}
