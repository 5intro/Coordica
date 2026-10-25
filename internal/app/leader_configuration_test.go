package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/store"
)

type leaderCatalogFake struct {
	Caps  []domain.Capability
	Calls int
}

func (f *leaderCatalogFake) DiscoverLeaders(context.Context) ([]domain.Capability, error) {
	f.Calls++
	return f.Caps, nil
}

func leaderCatalogFixture() (*leaderCatalogFake, domain.LeaderSelection) {
	choice := domain.LeaderSelection{Tool: "local-harness", Model: `["provider","flash"]`, ReasoningEffort: "high"}
	return &leaderCatalogFake{Caps: []domain.Capability{{Tool: choice.Tool, Installed: true, Available: true,
		Structured: true, Review: true, Cancel: true, ModelDiscovery: "reported",
		Models: []domain.ModelOption{{ID: choice.Model, ReasoningEfforts: []string{"high"}}}}}}, choice
}

func TestLeaderConfigurationOwnerCatalogVersionAndReplay(t *testing.T) {
	s, owner, _ := initFixture(t)
	catalog, choice := leaderCatalogFixture()
	s.LeaderCatalog = catalog
	ctx := context.Background()
	for _, actor := range []Actor{{ID: "chat", Role: "chat"}, {ID: "node", Role: "node"}} {
		if _, err := (LeaderConfigurations{s}).Read(ctx, actor); err == nil {
			t.Fatal("non-Owner read catalog")
		}
		if _, err := (LeaderConfigurations{s}).Configure(ctx, actor, "forbidden", domain.LeaderConfigureRequest{Selection: choice}); err == nil {
			t.Fatal("non-Owner configured Leader")
		}
	}
	if catalog.Calls != 0 {
		t.Fatal("unauthorized request performed discovery")
	}
	config, err := (LeaderConfigurations{s}).Read(ctx, owner)
	if err != nil || config.Selection != domain.DefaultLeaderSelection() || len(config.Capabilities) != 1 {
		t.Fatal(config, err)
	}
	if err := s.DB.View(ctx, func(tx *store.Tx) error {
		var stored domain.AutomationSettings
		if err := tx.Get("automation", "current", &stored); !errors.Is(err, domain.ErrNotFound) {
			t.Fatal("catalog read persisted configuration defaults", stored, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	req := domain.LeaderConfigureRequest{ExpectedVersion: config.Version, Selection: choice}
	raw, err := (LeaderConfigurations{s}).Configure(ctx, owner, "save-leader", req)
	saved := decodeTest[domain.AutomationSettings](t, raw, err)
	if saved.Enabled || leaderSelection(saved) != choice || saved.DevelopmentModel != "gpt-6.1-sol" {
		t.Fatal("selection granted unrelated authority", saved)
	}
	catalog.Caps = nil
	calls := catalog.Calls
	replay, err := (LeaderConfigurations{s}).Configure(ctx, owner, "save-leader", req)
	if err != nil || string(replay) != string(raw) || catalog.Calls != calls {
		t.Fatal("lost receipt required a vanished catalog", err)
	}
	restored, _ := leaderCatalogFixture()
	catalog.Caps = restored.Caps
	if _, err := (LeaderConfigurations{s}).Configure(ctx, owner, "stale", req); err == nil {
		t.Fatal("stale config accepted")
	}
	if _, err := (LeaderConfigurations{s}).Configure(ctx, owner, "save-leader", domain.LeaderConfigureRequest{
		ExpectedVersion: config.Version, Selection: domain.DefaultLeaderSelection(),
	}); err == nil {
		t.Fatal("idempotency key changed meaning")
	}
	project, _ := s.Project(ctx)
	if !project.Paused {
		t.Fatal("model configuration resumed project")
	}
}

func TestLeaderConfigurationRejectsUnreportedOrIncompatibleOptions(t *testing.T) {
	for _, kind := range []string{"missing tool", "missing model", "effort", "unavailable", "not reviewable", "no cancel", "unreported"} {
		t.Run(kind, func(t *testing.T) {
			s, owner, _ := initFixture(t)
			catalog, choice := leaderCatalogFixture()
			s.LeaderCatalog = catalog
			switch kind {
			case "missing tool":
				choice.Tool = "absent"
			case "missing model":
				choice.Model = "absent"
			case "effort":
				choice.ReasoningEffort = "ultra"
			case "unavailable":
				catalog.Caps[0].Available = false
			case "not reviewable":
				catalog.Caps[0].Review = false
			case "no cancel":
				catalog.Caps[0].Cancel = false
			case "unreported":
				catalog.Caps[0].ModelDiscovery = "unavailable"
			}
			if _, err := (LeaderConfigurations{s}).Configure(context.Background(), owner, "invalid", domain.LeaderConfigureRequest{
				ExpectedVersion: 1, Selection: choice,
			}); err == nil {
				t.Fatal("unverified Leader options accepted")
			}
			config, err := (LeaderConfigurations{s}).Read(context.Background(), owner)
			if err != nil || config.Version != 1 || config.Selection != domain.DefaultLeaderSelection() {
				t.Fatal(config, err)
			}
		})
	}
}

func TestInitializationLeaderChoiceIsAtomicAndReplaysAfterDiscoveryDisappears(t *testing.T) {
	s, owner, _ := initFixture(t)
	catalog, choice := leaderCatalogFixture()
	s.LeaderCatalog = catalog
	ctx := context.Background()
	config, _ := (LeaderConfigurations{s}).Read(ctx, owner)
	stale := config.Version + 1
	request := domain.InitializationRequest{Kind: "existing", Leader: &choice, LeaderVersion: &stale}
	if _, err := s.RequestInitialization(ctx, owner, "stale-init", request); err == nil {
		t.Fatal("stale Leader version accepted")
	}
	before, _ := s.Snapshot(ctx, owner)
	if before.Initialization != nil && before.Initialization.OperationID != "" || before.Automation.Version != config.Version {
		t.Fatal("failed initialization partially persisted a Leader or operation")
	}
	request.LeaderVersion = &config.Version
	raw, err := s.RequestInitialization(ctx, owner, "open-selected", request)
	opened := decodeTest[domain.Initialization](t, raw, err)
	if opened.Leader == nil || *opened.Leader != choice {
		t.Fatal("unbound baseline Leader", opened)
	}
	var operation domain.InitializationOperation
	if err := s.DB.View(ctx, func(tx *store.Tx) error {
		return tx.Get("initialization_operation", opened.OperationID, &operation)
	}); err != nil || operation.Leader == nil || *operation.Leader != choice {
		t.Fatal(operation, err)
	}
	catalog.Caps = nil
	replay, err := s.RequestInitialization(ctx, owner, "open-selected", request)
	if err != nil || string(raw) != string(replay) {
		t.Fatal("initialization replay required discovery", err)
	}
	after, _ := s.Snapshot(ctx, owner)
	if !after.Project.Paused || after.Automation.Enabled || len(after.Tasks) != 0 || len(after.Runs) != 0 {
		t.Fatal("initialization dispatched development")
	}
	if _, err := s.RequestInitialization(ctx, owner, "missing-pair", domain.InitializationRequest{Kind: "existing", Leader: &choice}); err == nil {
		t.Fatal("unversioned Leader choice accepted")
	}
}

type selectedLeaderFake struct {
	Choice domain.LeaderSelection
	Output string
	Err    error
}

func (f *selectedLeaderFake) Execute(context.Context, string, string, func(int) error) (PlanningOutcome, error) {
	return PlanningOutcome{}, errors.New("legacy fallback must not run")
}
func (f *selectedLeaderFake) ExecuteSelected(_ context.Context, choice domain.LeaderSelection, _, _ string, _ func(int) error) (PlanningOutcome, error) {
	f.Choice = choice
	return PlanningOutcome{TreeStopped: true, Output: f.Output, ElapsedSeconds: 1}, f.Err
}

func TestInitializationAndPlanningUsePersistedChoiceAfterConfigurationChanges(t *testing.T) {
	t.Run("initialization", func(t *testing.T) {
		s, owner, _ := initFixture(t)
		catalog, choice := leaderCatalogFixture()
		s.LeaderCatalog = catalog
		version := int64(1)
		_, err := s.RequestInitialization(context.Background(), owner, "open", domain.InitializationRequest{
			Kind: "existing", Leader: &choice, LeaderVersion: &version,
		})
		if err != nil {
			t.Fatal(err)
		}
		result := initResult(t, s, true)
		raw, _ := json.Marshal(result)
		changeStoredLeader(t, s)
		executor := &selectedLeaderFake{Output: string(raw)}
		if err := (&InitializationWorker{Service: s, Executor: executor}).Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
		current, _ := s.Initialization(context.Background(), owner)
		if executor.Choice != choice || current.Status != "awaiting_owner" {
			t.Fatal("choice changed or result lost", executor.Choice, current)
		}
	})
	t.Run("planning", func(t *testing.T) {
		f, d, result := planningFixture(t, true)
		catalog, choice := leaderCatalogFixture()
		f.s.LeaderCatalog = catalog
		config, _ := (LeaderConfigurations{f.s}).Read(context.Background(), f.owner)
		if _, err := (LeaderConfigurations{f.s}).Configure(context.Background(), f.owner, "select", domain.LeaderConfigureRequest{
			ExpectedVersion: config.Version, Selection: choice,
		}); err != nil {
			t.Fatal(err)
		}
		run := scheduleTest(t, f, d, "selected-run")
		changeStoredLeader(t, f.s)
		raw, _ := json.Marshal(result)
		executor := &selectedLeaderFake{Output: string(raw)}
		if err := (&PlanningWorker{Service: f.s, Executor: executor}).Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
		var current domain.PlanningRun
		_ = f.s.DB.View(context.Background(), func(tx *store.Tx) error { return tx.Get("planning_run", run.ID, &current) })
		if executor.Choice != choice || current.Leader == nil || *current.Leader != choice || current.ExecutionAdapter != choice.Tool {
			t.Fatal("queued Run followed a newer global model", executor.Choice, current)
		}
	})
}

func changeStoredLeader(t *testing.T, s *Service) {
	t.Helper()
	if err := s.DB.Update(context.Background(), func(tx *store.Tx) error {
		settings, err := automation(tx)
		if err != nil {
			return err
		}
		settings.LeaderTool, settings.LeaderModel, settings.LeaderReasoningEffort = "other", "another-model", ""
		settings.Version++
		return tx.Put("automation", "current", settings)
	}); err != nil {
		t.Fatal(err)
	}
}

func TestSelectedLeaderCannotFallBackToLegacyExecutor(t *testing.T) {
	f := newServiceFixture(t)
	called := false
	executor := planningExecFunc(func(context.Context, string, string) (PlanningOutcome, error) {
		called = true
		return PlanningOutcome{}, nil
	})
	_, choice := leaderCatalogFixture()
	ref := activityRun(t, f)
	out, err := executeObservedLeader(context.Background(), f.s, ref, executor, choice, "", "", nil)
	if err == nil || called || !out.TreeStopped {
		t.Fatal("selected model fell back", out, err)
	}
	if pinnedLeader(nil) != domain.DefaultLeaderSelection() {
		t.Fatal("legacy model changed")
	}
}

func TestInitializationDigestPreservesApprovedLegacyBaseline(t *testing.T) {
	legacy := domain.Initialization{Version: 3, Kind: "existing"}
	old := `{"Version":3,"Kind":"existing","SHA":"","Policy":"","Available":null,"Covered":null,"Omissions":null,"Result":null,"Feedback":null}`
	if initializationDigest(legacy) != Hash(old) {
		t.Fatal("approved legacy digest invalidated")
	}
	_, choice := leaderCatalogFixture()
	legacy.Leader = &choice
	if initializationDigest(legacy) == Hash(old) {
		t.Fatal("selected Leader omitted from new baseline digest")
	}
}

func TestSelectedLeaderResultStillRequiresStructuredEvidence(t *testing.T) {
	s, owner, _ := initFixture(t)
	catalog, choice := leaderCatalogFixture()
	s.LeaderCatalog = catalog
	version := int64(1)
	if _, err := s.RequestInitialization(context.Background(), owner, "open-invalid-result", domain.InitializationRequest{
		Kind: "existing", Leader: &choice, LeaderVersion: &version,
	}); err != nil {
		t.Fatal(err)
	}
	executor := &selectedLeaderFake{Output: `{"ready":true}`}
	if err := (&InitializationWorker{Service: s, Executor: executor}).Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	snapshot, err := s.Snapshot(context.Background(), owner)
	if err != nil || snapshot.Initialization.Status != "draft" || snapshot.Initialization.ApprovedDigest != "" ||
		!snapshot.Project.Paused || len(snapshot.Tasks) != 0 || snapshot.Automation.Enabled {
		t.Fatal("native tool selection weakened result validation or authority", snapshot.Initialization, err)
	}
}

func TestInitializationRetryAndRecoveryRetainLeaderAndCumulativeUsage(t *testing.T) {
	s, owner, _ := initFixture(t)
	catalog, choice := leaderCatalogFixture()
	s.LeaderCatalog = catalog
	ctx := context.Background()
	version := int64(1)
	if _, err := s.RequestInitialization(ctx, owner, "retry-selected", domain.InitializationRequest{
		Kind: "existing", Leader: &choice, LeaderVersion: &version,
	}); err != nil {
		t.Fatal(err)
	}
	executor := &selectedLeaderFake{Err: errors.New("transient model error")}
	worker := &InitializationWorker{Service: s, Executor: executor}
	if err := worker.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	current, _ := s.Initialization(ctx, owner)
	if current.OperationGeneration != 2 || current.OperationState != "queued" {
		t.Fatal("bounded retry not queued", current)
	}
	changeStoredLeader(t, s)
	if err := worker.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	result := initResult(t, s, true)
	encoded, _ := json.Marshal(result)
	executor.Err, executor.Output = nil, string(encoded)
	if err := worker.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	var operation domain.InitializationOperation
	err := s.DB.View(ctx, func(tx *store.Tx) error { return tx.Get("initialization_operation", current.OperationID, &operation) })
	if err != nil || executor.Choice != choice || operation.Attempt != 2 || operation.UsedSeconds < 2 ||
		operation.Leader == nil || *operation.Leader != choice || !operation.Settled || operation.State != "completed" {
		t.Fatal("retry lost model, accounting, or stop evidence", operation, err)
	}
}
