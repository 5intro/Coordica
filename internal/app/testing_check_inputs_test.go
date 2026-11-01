package app

import (
	"context"
	"reflect"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func TestMergeSuiteNewInputInvalidatesAndReattestsFixedBaseline(t *testing.T) {
	for _, enable := range []bool{false, true} {
		t.Run(map[bool]string{false: "add", true: "enable"}[enable], func(t *testing.T) {
			ctx := context.Background()
			s, owner, source := initFixture(t)
			suite := domain.TestSuite{ID: "build", Name: "Build", Stage: "merge", Enabled: !enable,
				MatchPaths: []string{"src/**"}, Checks: []domain.Check{{Name: "build", Command: "builder", TimeoutSeconds: 30,
					RequiredPaths: []string{"package-lock.json"}}}}
			if enable {
				raw, err := s.SaveTestSuite(ctx, owner, "disabled-suite", domain.TestSuiteSave{Suite: suite})
				suite = decodeTest[domain.TestSuite](t, raw, err)
			}
			requestInit(t, s, owner, "existing")
			previous := analyzeInit(t, s, owner, initResult(t, s, true))
			if _, err := s.ConfirmInitialization(ctx, owner, "confirm-old", domain.InitializationConfirm{
				ExpectedVersion: previous.Version, Digest: previous.Digest}); err != nil {
				t.Fatal(err)
			}
			suite.Enabled = true
			in := domain.TestSuiteSave{Suite: suite, ExpectedVersion: suite.Version}
			raw, err := s.SaveTestSuite(ctx, owner, "new-input", in)
			saved := decodeTest[domain.TestSuite](t, raw, err)
			after, err := s.Initialization(ctx, owner)
			if err != nil || after.Status != "draft" || after.Version != previous.Version+1 ||
				after.ApprovedVersion != 0 || after.ApprovedBaseline != nil || after.Digest != "" {
				t.Fatal("unattested input reused an approved baseline", after, err)
			}
			if err = s.DB.View(ctx, func(tx *store.Tx) error { return baselineGate(tx, nil, false) }); err == nil {
				t.Fatal("planning/execution gate accepted incomplete input attestations")
			}
			if _, err = s.ConfirmInitialization(ctx, owner, "stale-confirm", domain.InitializationConfirm{
				ExpectedVersion: previous.Version, Digest: previous.Digest}); err == nil {
				t.Fatal("old confirmation restored an incomplete baseline")
			}
			replay, err := s.SaveTestSuite(ctx, owner, "new-input", in)
			unchanged, _ := s.Initialization(ctx, owner)
			if err != nil || string(replay) != string(raw) || !reflect.DeepEqual(unchanged, after) {
				t.Fatal("idempotent suite save invalidated twice", err)
			}
			s.Source = checkInputAnalysisFake{source}
			requestInit(t, s, owner, "existing")
			fresh := analyzeInit(t, s, owner, initResult(t, s, true))
			if !reflect.DeepEqual(fresh.CheckInputPaths, []string{"package-lock.json"}) {
				t.Fatal("new analysis failed to attest omitted manifest", fresh)
			}
			if _, err = s.ConfirmInitialization(ctx, owner, "confirm-fresh", domain.InitializationConfirm{
				ExpectedVersion: fresh.Version, Digest: fresh.Digest}); err != nil {
				t.Fatal(err)
			}
			proposals := map[string]domain.TaskProposal{"source": {Key: "source", Paths: []string{"src/module.go"}}}
			if err = validatePlanningCheckInputs(nil, fresh, proposals, nil, saved); err != nil {
				t.Fatal("existing lockfile incorrectly exhausted replanning retries", err)
			}
		})
	}
}

func TestMergeSuiteKnownInputsPreserveBaselineAndUnknownInputsRollback(t *testing.T) {
	ctx := context.Background()
	s, owner, _ := initFixture(t)
	requestInit(t, s, owner, "existing")
	v := analyzeInit(t, s, owner, initResult(t, s, true))
	if _, err := s.ConfirmInitialization(ctx, owner, "confirm", domain.InitializationConfirm{
		ExpectedVersion: v.Version, Digest: v.Digest}); err != nil {
		t.Fatal(err)
	}
	before, _ := s.Initialization(ctx, owner)
	suite := domain.TestSuite{ID: "source", Name: "Source", Enabled: true, Stage: "merge",
		Checks: []domain.Check{{Name: "compile", Command: "builder", TimeoutSeconds: 30, RequiredPaths: []string{"src"}}}}
	raw, err := s.SaveTestSuite(ctx, owner, "known-input", domain.TestSuiteSave{Suite: suite})
	suite = decodeTest[domain.TestSuite](t, raw, err)
	after, _ := s.Initialization(ctx, owner)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("publicly inventoried input unnecessarily invalidated baseline")
	}
	suite.Checks[0].RequiredPaths = []string{"go.sum"}
	suite.RequirementIDs = []string{"missing-requirement"}
	if _, err = s.SaveTestSuite(ctx, owner, "invalid-suite", domain.TestSuiteSave{Suite: suite, ExpectedVersion: suite.Version}); err == nil {
		t.Fatal("invalid suite accepted")
	}
	after, _ = s.Initialization(ctx, owner)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("failed suite mutation leaked baseline invalidation")
	}
}

func TestMergeSuiteMutationCannotRaceInitializationInputSnapshot(t *testing.T) {
	ctx := context.Background()
	s, owner, _ := initFixture(t)
	before := requestInit(t, s, owner, "existing")
	suite := domain.TestSuite{ID: "build", Name: "Build", Stage: "merge", Enabled: true,
		Checks: []domain.Check{{Name: "build", Command: "builder", TimeoutSeconds: 30, RequiredPaths: []string{"go.sum"}}}}
	if _, err := s.SaveTestSuite(ctx, owner, "race", domain.TestSuiteSave{Suite: suite}); err == nil {
		t.Fatal("suite contract changed during initialization")
	}
	after, _ := s.Initialization(ctx, owner)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("rejected concurrent mutation changed initialization")
	}
}
