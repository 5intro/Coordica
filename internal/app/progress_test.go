package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func progressClaim(t *testing.T, f *serviceFixture, key string) *domain.Assignment {
	t.Helper()
	b, err := f.s.Claim(context.Background(), f.node, key, domain.ClaimRequest{
		ProgressVersion: domain.ProgressVersion, AssignmentContextVersion: domain.AssignmentContextVersion,
		Capabilities: recoveryCapabilities("codex"),
	}, strings.Repeat("a", 40))
	return decodeTest[*domain.Assignment](t, b, err)
}

func progressFixture(t *testing.T) (*serviceFixture, *domain.Assignment) {
	t.Helper()
	f := newServiceFixture(t)
	setTestTimeLimits(t, f.s, false)
	f.s.Lease = 4 * time.Hour // Fake-time test only; production leases stay short.
	a := progressClaim(t, f, "progress-claim")
	if a == nil || !domain.ProgressManaged(a.Run) || a.Run.ProgressCheckSeconds != 1800 {
		t.Fatalf("default progress contract missing: %+v", a)
	}
	return f, a
}

func TestProgressDefaultIgnoresLegacyAllowancesAndRequiresCompatibleRunner(t *testing.T) {
	f := newServiceFixture(t)
	setTestTimeLimits(t, f.s, false)
	if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
		var task domain.Task
		if err := tx.Get("task", f.task.ID, &task); err != nil {
			return err
		}
		task.UsedSeconds = task.BudgetSeconds + 500
		return tx.Put("task", task.ID, task)
	}); err != nil {
		t.Fatal(err)
	}
	if old := f.claim(t, "old-runner"); old != nil {
		t.Fatal("old Runner received an unsupported deadline-free contract")
	}
	a := progressClaim(t, f, "new-runner")
	if a == nil || !domain.ProgressManaged(a.Run) || a.Task.ReservedSeconds != 0 {
		t.Fatal("legacy allowance still gates new development", a)
	}
}

func TestProgressLongDevelopmentCompletesWithoutDebitingOtherReservations(t *testing.T) {
	f, a := progressFixture(t)
	ctx := context.Background()
	if err := f.s.DB.Update(ctx, func(tx *store.Tx) error {
		p, err := getProject(tx)
		if err != nil {
			return err
		}
		p.ReservedSeconds = 73 // An unrelated finite operation is in flight.
		return tx.Put("project", p.ID, p)
	}); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(2 * time.Hour)
	response, err := f.s.Heartbeat(ctx, f.node, a.Run.ID, domain.HeartbeatRequest{
		Generation: a.Run.Generation, ElapsedSeconds: 1 << 60,
		Progress: &domain.ProgressObservation{Sequence: 4, ToolCompleted: 2},
	})
	if err != nil || response.Cancel {
		t.Fatal("healthy long development cancelled", response, err)
	}
	artifact, err := f.s.UploadArtifact(ctx, f.node, a.Run.ID, strings.NewReader("fixed bundle fixture"))
	if err != nil {
		t.Fatal(err)
	}
	req := domain.CompleteRequest{Generation: a.Run.Generation, Status: "succeeded",
		Head: strings.Repeat("b", 40), ArtifactID: artifact.ID, Quiescent: true, ElapsedSeconds: 7200}
	if _, err = f.s.Complete(ctx, f.node, "progress-done", a.Run.ID, req); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.Complete(ctx, f.node, "progress-done", a.Run.ID, req); err != nil {
		t.Fatal(err)
	}
	snapshot := f.snapshot(t)
	if len(snapshot.Candidates) != 1 || snapshot.Project.UsedSeconds != 7200 || snapshot.Project.ReservedSeconds != 73 ||
		snapshot.Tasks[0].ReservedSeconds != 0 || snapshot.Tasks[0].HoldReason != "" {
		t.Fatal("telemetry became an allowance, double charge or reservation debit", snapshot.Project, snapshot.Tasks)
	}
}

func TestProgressSilenceReplayAndActivityAreObservationsNotFailure(t *testing.T) {
	f, a := progressFixture(t)
	ctx := context.Background()
	beat := func(observation *domain.ProgressObservation) {
		t.Helper()
		response, err := f.s.Heartbeat(ctx, f.node, a.Run.ID,
			domain.HeartbeatRequest{Generation: a.Run.Generation, Progress: observation})
		if err != nil || response.Cancel {
			t.Fatal(response, err)
		}
	}
	f.now = f.now.Add(30 * time.Minute)
	beat(&domain.ProgressObservation{Sequence: 1})
	first := f.snapshot(t).Runs[0].Progress
	if first == nil || first.State != "needs_confirmation" {
		t.Fatal(first)
	}
	f.now = f.now.Add(time.Minute)
	beat(&domain.ProgressObservation{Sequence: 1, Messages: 20})
	afterReplay := f.snapshot(t)
	if !afterReplay.Runs[0].Progress.CheckedAt.Equal(first.CheckedAt) || afterReplay.Tasks[0].Retries != 0 {
		t.Fatal("duplicate refreshed observation or spent retry")
	}
	f.now = f.now.Add(29 * time.Minute)
	beat(&domain.ProgressObservation{Sequence: 2, ToolStarted: 1, ToolCompleted: 1})
	progress := f.snapshot(t).Runs[0].Progress
	if progress.State != "activity_observed" {
		t.Fatal(progress)
	}
	f.now = f.now.Add(30 * time.Minute)
	beat(nil) // No status reply is not evidence of failure.
	if f.snapshot(t).Tasks[0].HoldReason != "" {
		t.Fatal("missing reply stopped work")
	}
	_, err := f.s.Heartbeat(ctx, f.node, a.Run.ID, domain.HeartbeatRequest{Generation: a.Run.Generation,
		Progress: &domain.ProgressObservation{Sequence: 3, ToolCompleted: 0}})
	if err == nil {
		t.Fatal("regressing counters were accepted")
	}
}

func TestProgressLeaseExpiryRequiresStopAndFencesLateCompletion(t *testing.T) {
	f, first := progressFixture(t)
	ctx := context.Background()
	f.now = first.Run.LeaseUntil.Add(time.Second)
	if err := f.s.Expire(ctx); err != nil {
		t.Fatal(err)
	}
	if next := progressClaim(t, f, "before-stop"); next != nil {
		t.Fatal("unknown old process duplicated")
	}
	f.confirmRecoveryStop(t, first.Run.ID, "confirmed-stop")
	second := progressClaim(t, f, "after-stop")
	if second == nil || second.Run.Generation <= first.Run.Generation {
		t.Fatal("reassignment missing")
	}
	before := f.snapshot(t)
	if _, err := f.s.Complete(ctx, f.node, "late-result", first.Run.ID, domain.CompleteRequest{
		Generation: first.Run.Generation, Status: "succeeded", Head: strings.Repeat("b", 40), Quiescent: true,
	}); err != nil {
		t.Fatal(err)
	}
	after := f.snapshot(t)
	if after.Tasks[0].CurrentRunID != second.Run.ID || after.Tasks[0].Retries != 1 ||
		after.Project.UsedSeconds != before.Project.UsedSeconds || len(after.Candidates) != 0 {
		t.Fatal("old result affected replacement", after.Tasks)
	}
	// Unknown-process time records only the expired authorized window.
	if before.Project.UsedSeconds != 4*3600 {
		t.Fatal("lost execution invented unlimited charges", before.Project)
	}
}

func TestProgressPlanningAndGoalAccountingIgnoreCumulativeLifetimeByDefault(t *testing.T) {
	f, goal, result := planningFixture(t, true)
	setTestTimeLimits(t, f.s, false)
	ctx := context.Background()
	if err := f.s.DB.Update(ctx, func(tx *store.Tx) error {
		goal.BudgetSeconds, goal.UsedSeconds, goal.PlanningUsedSeconds = 0, 20000, 9000
		return tx.Put("planning_document", "current", goal)
	}); err != nil {
		t.Fatal(err)
	}
	result.Tasks[0].BudgetSeconds, result.Tasks[0].MaxRunSeconds = 0, 0
	scheduleTest(t, f, goal, "long-lived-goal")
	worker := &PlanningWorker{Service: f.s, Executor: resultExecutor(result)}
	if err := worker.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	snapshot := f.snapshot(t)
	if snapshot.PlanningDocument.Status != "admitted" || len(snapshot.Decisions) != 0 || snapshot.PlanningDocument.ReservedSeconds != 0 {
		t.Fatal("planning lifetime cap retained", snapshot.PlanningDocument, snapshot.Decisions)
	}
	f.s.Lease = 4 * time.Hour
	a := progressClaim(t, f, "goal-progress")
	if a == nil || !domain.ProgressManaged(a.Run) {
		t.Fatal("zero-budget task not admitted", a)
	}
	before := f.snapshot(t).PlanningDocument.UsedSeconds
	f.now = f.now.Add(2 * time.Hour)
	response, err := f.s.Heartbeat(ctx, f.node, a.Run.ID, domain.HeartbeatRequest{Generation: a.Run.Generation})
	if err != nil || response.Cancel {
		t.Fatal(response, err)
	}
	after := f.snapshot(t)
	if after.PlanningDocument.UsedSeconds != before+7200 || after.PlanningDocument.ReservedSeconds != 0 {
		t.Fatal("goal telemetry charged a nonexistent reservation", after.PlanningDocument)
	}
}

func TestProgressSettingsValidateIntervalAndKeepRetryBounds(t *testing.T) {
	f := newServiceFixture(t)
	for _, seconds := range []int64{-1, domain.MaxProgressCheckSeconds + 1} {
		settings := f.snapshot(t).Automation
		settings.ProgressCheckSeconds = seconds
		if _, err := f.s.ConfigureAutomation(context.Background(), f.owner, ID("invalid-progress"),
			domain.AutomationRequest{ExpectedVersion: settings.Version, Settings: settings}); err == nil {
			t.Fatal("invalid interval saved", seconds)
		}
	}
	settings := f.snapshot(t).Automation
	settings.EnforceTimeLimits, settings.ProgressCheckSeconds, settings.PlanningBudgetSeconds = false, 2700, 0
	if _, err := f.s.ConfigureAutomation(context.Background(), f.owner, "progress-settings",
		domain.AutomationRequest{ExpectedVersion: settings.Version, Settings: settings}); err != nil {
		t.Fatal(err)
	}
	a := progressClaim(t, f, "configured-progress")
	if a == nil || a.Run.ProgressCheckSeconds != 2700 {
		t.Fatal("configured interval not pinned", a)
	}
	settings = f.snapshot(t).Automation
	settings.PlanningMaxRetries = 3
	if _, err := f.s.ConfigureAutomation(context.Background(), f.owner, "too-many-retries",
		domain.AutomationRequest{ExpectedVersion: settings.Version, Settings: settings}); err == nil {
		t.Fatal("retry cap removed")
	}
}

func TestProgressPauseStillCancelsWithoutRecordingNewActivity(t *testing.T) {
	f, a := progressFixture(t)
	ctx := context.Background()
	if _, err := f.s.TaskAction(ctx, f.owner, "pause-progress", a.Task.ID, "pause",
		domain.VersionRequest{ExpectedVersion: a.Task.Version}); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(30 * time.Minute)
	response, err := f.s.Heartbeat(ctx, f.node, a.Run.ID, domain.HeartbeatRequest{Generation: a.Run.Generation,
		Progress: &domain.ProgressObservation{Sequence: 1, Messages: 1}})
	if err != nil || !response.Cancel {
		t.Fatal("pause did not cancel development", response, err)
	}
	if f.snapshot(t).Runs[0].Progress != nil {
		t.Fatal("revoked execution refreshed progress")
	}
}

func TestProgressAutomaticReassignmentStillStopsAtRetryLimit(t *testing.T) {
	f, a := progressFixture(t)
	ctx := context.Background()
	if err := f.s.DB.Update(ctx, func(tx *store.Tx) error {
		var task domain.Task
		if err := tx.Get("task", a.Task.ID, &task); err != nil {
			return err
		}
		task.Retries = task.MaxRetries
		return tx.Put("task", task.ID, task)
	}); err != nil {
		t.Fatal(err)
	}
	f.now = a.Run.LeaseUntil.Add(time.Second)
	if err := f.s.Expire(ctx); err != nil {
		t.Fatal(err)
	}
	f.confirmRecoveryStop(t, a.Run.ID, "retry-limit-stop")
	if next := progressClaim(t, f, "retry-limit-claim"); next != nil {
		t.Fatal("retry cap ignored")
	}
	task := f.snapshot(t).Tasks[0]
	if task.HoldReason != "human_decision" || task.Retries != task.MaxRetries {
		t.Fatal(task)
	}
}
