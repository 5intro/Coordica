package codequality

import (
	"errors"
	"testing"
)

func TestAnalysisErrorsDistinguishCandidateFromFrozenBaseline(t *testing.T) {
	bad := []File{{Path: "src/broken.go", Text: "package p\nfunc broken(\n"}}
	_, err := Regressions(nil, bad)
	var candidate *CandidateError
	if !errors.As(err, &candidate) {
		t.Fatal("candidate syntax defect lost its identity", err)
	}
	_, err = Regressions(bad, nil)
	if err != nil {
		t.Fatal("deleting invalid baseline source must be permitted", err)
	}
}
