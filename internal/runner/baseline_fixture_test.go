package runner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"coordica/internal/app"
	"coordica/internal/domain"
)

type runnerBaselineExecutor struct {
	Baseline domain.Baseline
	Bound    domain.Initialization
}

func (f runnerBaselineExecutor) Execute(_ context.Context, dir, _ string, _ func(int) error) (app.PlanningOutcome, error) {
	raw, e := os.ReadFile(filepath.Join(dir, "source-manifest.json"))
	if e != nil {
		return app.PlanningOutcome{TreeStopped: true}, e
	}
	var manifest struct {
		Paths []string `json:"paths"`
	}
	if e = json.Unmarshal(raw, &manifest); e != nil {
		return app.PlanningOutcome{TreeStopped: true}, e
	}
	facts := []domain.AnalysisEvidence{}
	for _, path := range manifest.Paths {
		if _, e = os.ReadFile(filepath.Join(dir, "source", filepath.FromSlash(path))); e != nil {
			return app.PlanningOutcome{TreeStopped: true}, e
		}
		facts = append(facts, domain.AnalysisEvidence{Path: path, Fact: "Fixture read-only source inspected"})
	}
	result := domain.InitializationResult{Generation: f.Bound.OperationGeneration, InitializationVersion: f.Bound.Version, OperationID: f.Bound.OperationID, SourceSHA: f.Bound.SourceSHA, Ready: true, InspectedPaths: manifest.Paths, Facts: facts, Suggestions: []string{}, Questions: []domain.InitializationQuestion{}, Baseline: f.Baseline}
	raw, e = json.Marshal(result)
	return app.PlanningOutcome{TreeStopped: true, ElapsedSeconds: 1, Output: string(raw)}, e
}
func seedRunnerBaseline(t *testing.T, s *app.Service) {
	t.Helper()
	ctx := context.Background()
	owner := app.Actor{ID: "owner", Role: "owner"}
	p, e := s.Project(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.RequestInitialization(ctx, owner, "fixture-init", domain.InitializationRequest{Kind: "existing"}); e != nil {
		t.Fatal(e)
	}
	baseline := domain.Baseline{Requirements: "Fixture", Acceptance: "Fixture", Architecture: "Fixture", Modules: "Fixture", Conventions: "Fixture", Testing: "Fixture", Functionality: "Fixture", Risks: "Fixture", AllowedRoots: []string{"."}, Checks: p.Checks, ProtectedPaths: p.ProtectedPaths}
	bound, _ := s.Initialization(ctx, owner)
	worker := app.InitializationWorker{Service: s, Executor: runnerBaselineExecutor{baseline, bound}}
	if e = worker.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	v, e := s.Initialization(ctx, owner)
	if e != nil {
		t.Fatal(e)
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
