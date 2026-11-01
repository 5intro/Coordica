package app

import (
	"context"
	"reflect"
	"testing"

	"coordica/internal/domain"
)

type checkInputAnalysisFake struct{ *analysisSourceFake }

func (f checkInputAnalysisFake) CheckInputPaths(_ context.Context, _, sha string, inputs []string) ([]string, error) {
	if sha != f.SHA || !reflect.DeepEqual(inputs, []string{"package-lock.json"}) {
		return nil, domain.Fail("test", "wrong fixed input query")
	}
	return []string{"package-lock.json"}, nil
}

func TestInitializationPinsRequiredInputPresenceWithoutDisclosingContents(t *testing.T) {
	checks := []domain.Check{{Name: "compile", Command: "builder", Args: []string{}, RequiredPaths: []string{"package-lock.json"},
		OutputPaths: []string{"build/**"}, TimeoutSeconds: 30}}
	s, owner, source := initFixtureChecks(t, checks)
	s.Source = checkInputAnalysisFake{source}
	requestInit(t, s, owner, "existing")
	v := analyzeInit(t, s, owner, initResult(t, s, true))
	if v.Status != "awaiting_owner" || !reflect.DeepEqual(v.CheckInputPaths, []string{"package-lock.json"}) || len(v.AvailablePaths) != 2 {
		t.Fatal("fixed input presence was lost or private content added to analysis", v)
	}
	changed := v
	changed.CheckInputPaths = nil
	if initializationDigest(changed) == v.Digest {
		t.Fatal("input presence was not bound to Owner confirmation")
	}
	if _, err := s.ConfirmInitialization(context.Background(), owner, "confirm-inputs",
		domain.InitializationConfirm{ExpectedVersion: v.Version, Digest: v.Digest}); err != nil {
		t.Fatal(err)
	}
	v = requestInit(t, s, owner, "existing")
	if len(v.CheckInputPaths) != 0 {
		t.Fatal("new analysis inherited old input presence", v.CheckInputPaths)
	}
}

func TestInitializationCannotInventRequiredInputPresenceFromAnUnsupportedSource(t *testing.T) {
	s, owner, _ := initFixtureChecks(t, []domain.Check{{Name: "compile", Command: "builder",
		RequiredPaths: []string{"entry"}, TimeoutSeconds: 30}})
	requestInit(t, s, owner, "existing")
	called := false
	w := InitializationWorker{Service: s, Executor: planningExecFunc(func(context.Context, string, string) (PlanningOutcome, error) {
		called = true
		return PlanningOutcome{TreeStopped: true}, nil
	})}
	if err := w.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	v, err := s.Initialization(context.Background(), owner)
	if err != nil || called || v.Status == "awaiting_owner" || v.ApprovedVersion != 0 {
		t.Fatal("unattested inputs reached readiness or a model run", called, v, err)
	}
}
