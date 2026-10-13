package httpapi

import (
	"context"
	"encoding/json"
	"testing"

	"coordica/internal/app"
	"coordica/internal/domain"
	"coordica/internal/gitops"
)

type fixtureUnbornSource struct{}

func (fixtureUnbornSource) AnalysisRevision(context.Context, string, string) (string, error) {
	return "", gitops.ErrUnborn
}
func (fixtureUnbornSource) ReadAnalysis(context.Context, string, string) (gitops.AnalysisSnapshot, error) {
	panic("unborn fixture has no source snapshot")
}

type fixtureBaselineExecutor struct{ Result domain.InitializationResult }

func (f fixtureBaselineExecutor) Execute(context.Context, string, string, func(int) error) (app.PlanningOutcome, error) {
	for i := range f.Result.Baseline.Checks {
		if f.Result.Baseline.Checks[i].Args == nil {
			f.Result.Baseline.Checks[i].Args = []string{}
		}
	}
	raw, _ := json.Marshal(f.Result)
	return app.PlanningOutcome{TreeStopped: true, Output: string(raw), ElapsedSeconds: 1}, nil
}
func seedHTTPBaseline(t *testing.T, s *app.Service) {
	t.Helper()
	ctx := context.Background()
	owner := app.Actor{ID: "owner", Role: "owner"}
	p, e := s.Project(ctx)
	if e != nil {
		t.Fatal(e)
	}
	prior := s.Source
	s.Source = fixtureUnbornSource{}
	defer func() { s.Source = prior }()
	if _, e = s.RequestInitialization(ctx, owner, "fixture-init", domain.InitializationRequest{Kind: "new"}); e != nil {
		t.Fatal(e)
	}
	b := domain.Baseline{Requirements: "Fixture", Acceptance: "Fixture", Architecture: "Fixture", Modules: "Fixture", Conventions: "Fixture", Testing: "Fixture", Functionality: "Fixture", Risks: "Fixture", AllowedRoots: []string{"."}, Checks: p.Checks, ProtectedPaths: p.ProtectedPaths}
	bound, _ := s.Initialization(ctx, owner)
	w := app.InitializationWorker{Service: s, Executor: fixtureBaselineExecutor{domain.InitializationResult{Generation: bound.OperationGeneration, InitializationVersion: bound.Version, OperationID: bound.OperationID, SourceSHA: bound.SourceSHA, Ready: true, InspectedPaths: []string{}, Facts: []domain.AnalysisEvidence{}, Suggestions: []string{}, Questions: []domain.InitializationQuestion{}, Baseline: b}}}
	if e = w.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	v, e := s.Initialization(ctx, owner)
	if e != nil {
		t.Fatal(e)
	}
	if v.Status != "awaiting_owner" {
		t.Fatalf("fixture analysis failed: %+v", v)
	}
	if _, e = s.ConfirmInitialization(ctx, owner, "fixture-confirm", domain.InitializationConfirm{ExpectedVersion: v.Version, Digest: v.Digest}); e != nil {
		t.Fatal(e)
	}
	p, e = s.Project(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ProjectAction(ctx, owner, "fixture-resume", "resume", domain.VersionRequest{ExpectedVersion: p.Version}); e != nil {
		t.Fatal(e)
	}
}
