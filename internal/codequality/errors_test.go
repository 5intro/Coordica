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
	if err == nil || errors.As(err, &candidate) {
		t.Fatal("baseline defect misclassified as a candidate repair", err)
	}
}
