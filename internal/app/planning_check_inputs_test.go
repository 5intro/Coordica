package app

import (
	"context"
	"strings"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func TestPlanningCheckInputsCannotComeFromDownstreamOrParallelTasks(t *testing.T) {
	check := []domain.Check{{Name: "compile", RequiredPaths: []string{"src/entry.ts"}}}
	for _, mode := range []string{"downstream", "parallel", "unowned", "own", "ancestor", "baseline", "omitted manifest"} {
		t.Run(mode, func(t *testing.T) {
			a := domain.TaskProposal{Key: "library", Paths: []string{"src/library/**"}}
			b := domain.TaskProposal{Key: "entry", Paths: []string{"src/entry.ts"}, Dependencies: []string{"library"}}
			baseline := domain.Initialization{}
			switch mode {
			case "parallel":
				b.Dependencies = nil
			case "unowned":
				b.Paths = []string{"test/**"}
			case "own":
				a.Paths = []string{"src/**"}
			case "ancestor":
				a.Dependencies, b.Dependencies = []string{"entry"}, nil
			case "baseline":
				baseline.AvailablePaths = []string{"src/entry.ts"}
			case "omitted manifest":
				baseline.CheckInputPaths = []string{"src/entry.ts"}
			}
			err := validatePlanningCheckInputs(check, baseline, map[string]domain.TaskProposal{"library": a, "entry": b}, nil)
			wantFailure := mode == "downstream" || mode == "parallel" || mode == "unowned"
			if (err != nil) != wantFailure {
				t.Fatal(mode, err)
			}
		})
	}
}

func planningInputFixture(t *testing.T) (*serviceFixture, domain.PlanningDocument, domain.LeaderResult) {
	t.Helper()
	f, goal, result := planningFixture(t, true)
	if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
		p, err := getProject(tx)
		if err != nil {
			return err
		}
		p.Checks[0].RequiredPaths = []string{"src/entry.ts"}
		return tx.Put("project", p.ID, p)
	}); err != nil {
		t.Fatal(err)
	}
	seedCurrentPolicyBaseline(t, f.s)
	result.Tasks[0].Paths = []string{"src/library/**"}
	return f, goal, result
}

func TestPlanningInputRejectionRollsBackBatchAndReplansWithinOriginalLimit(t *testing.T) {
	f, goal, invalid := planningInputFixture(t)
	before := f.snapshot(t)
	scheduleTest(t, f, goal, "input-contract")
	w := &PlanningWorker{Service: f.s, Executor: resultExecutor(invalid)}
	if err := w.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	after := f.snapshot(t)
	if len(after.Tasks) != len(before.Tasks) || len(after.PlanningRuns) != 2 || len(after.Decisions) != len(before.Decisions) {
		t.Fatal("rejection partially wrote tasks or requested a premature Owner decision", after)
	}
	var queued domain.PlanningRun
	for _, r := range after.PlanningRuns {
		if r.State == "queued" {
			queued = r
		}
	}
	if queued.Attempt != 1 || !strings.Contains(queued.Detail, "src/entry.ts") || queued.PendingResult != nil {
		t.Fatal("retry omitted bounded feedback or retained a stale result", queued)
	}
	fixed := invalid
	fixed.Tasks[0].Paths = []string{"src/**"}
	w.Executor = planningExecFunc(func(_ context.Context, _ string, prompt string) (PlanningOutcome, error) {
		if !strings.Contains(prompt, "previous_rejection") || !strings.Contains(prompt, "src/entry.ts") {
			t.Fatal("native replanning did not receive the exact input failure")
		}
		return resultExecutor(fixed).Execute(context.Background(), "", prompt, nil)
	})
	if err := w.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	after = f.snapshot(t)
	if after.PlanningDocument.Status != "admitted" || len(after.Tasks) != len(before.Tasks)+1 ||
		after.Project.UsedSeconds <= before.Project.UsedSeconds || after.Project.ReservedSeconds != 0 {
		t.Fatal("corrected plan did not preserve atomic admission and cumulative accounting", after)
	}
}

func TestPlanningInputRejectionsStopAtSharedRetryLimit(t *testing.T) {
	f, goal, invalid := planningInputFixture(t)
	before := len(f.snapshot(t).Tasks)
	scheduleTest(t, f, goal, "bad-plan-limit")
	w := &PlanningWorker{Service: f.s, Executor: resultExecutor(invalid)}
	for range 5 {
		if err := w.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	s := f.snapshot(t)
	if len(s.PlanningRuns) != 3 || len(s.Tasks) != before || len(s.Decisions) != 1 || s.Decisions[0].Kind != "invalid_plan" {
		t.Fatal("invalid planning refreshed retries or leaked partial tasks", s)
	}
}

func TestCheckMetadataIsPartOfExactOwnerPolicyAgreement(t *testing.T) {
	p := domain.Project{Checks: []domain.Check{{Name: "compile", Command: "builder", Args: []string{}, TimeoutSeconds: 30,
		RequiredPaths: []string{"entry"}, OutputPaths: []string{"build/**"}}}}
	for _, change := range []string{"input", "output"} {
		b := domain.Baseline{Checks: append([]domain.Check{}, p.Checks...)}
		if change == "input" {
			b.Checks[0].RequiredPaths = nil
		} else {
			b.Checks[0].OutputPaths = []string{"**"}
		}
		if err := baselinePolicyAgreement(b, p); err == nil {
			t.Fatal("Leader changed trusted check paths without Owner policy mutation", change)
		}
	}
}

func TestPlanningCheckInputsRespectMergeSuiteApplicability(t *testing.T) {
	proposals := map[string]domain.TaskProposal{"core": {Key: "core", Paths: []string{"core/module.ts"}}}
	suite := domain.TestSuite{Enabled: true, Stage: "merge", MatchPaths: []string{"core/**"},
		Checks: []domain.Check{{Name: "compile", RequiredPaths: []string{"ui/entry.ts"}}}}
	if err := validatePlanningCheckInputs(nil, domain.Initialization{}, proposals, nil, suite); err == nil {
		t.Fatal("applicable merge input was not checked")
	}
	for _, stage := range []string{"project", "release"} {
		suite.Stage = stage
		if err := validatePlanningCheckInputs(nil, domain.Initialization{}, proposals, nil, suite); err != nil {
			t.Fatal("later verification prevented module admission", err)
		}
	}
	suite.Stage, suite.MatchPaths = "merge", []string{"other/**"}
	if err := validatePlanningCheckInputs(nil, domain.Initialization{}, proposals, nil, suite); err == nil {
		t.Fatal("unknown-path fallback was omitted from planning")
	}
	known := domain.TestSuite{ID: "known", Enabled: true, Stage: "merge", MatchPaths: []string{"core/**"}, Checks: []domain.Check{{Name: "unit"}}}
	if err := validatePlanningCheckInputs(nil, domain.Initialization{}, proposals, nil, suite, known); err != nil {
		t.Fatal("known scope selected an unrelated suite", err)
	}
	proposals["core"] = domain.TaskProposal{Key: "core", Paths: []string{"core/**"}}
	if err := validatePlanningCheckInputs(nil, domain.Initialization{}, proposals, nil, suite, known); err != nil {
		t.Fatal("exact known task glob selected unrelated suites", err)
	}
	if !planningScopeCovers([]string{"core/module.ts"}, "core") {
		t.Fatal("permitted descendant cannot satisfy a required directory")
	}
}
