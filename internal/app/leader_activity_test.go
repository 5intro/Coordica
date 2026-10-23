package app

import (
	"context"
	"coordica/internal/domain"
	"coordica/internal/store"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func activityRun(t *testing.T, f *serviceFixture) domain.LeaderExecutionRef {
	t.Helper()
	run := domain.PlanningRun{ID: "activity_run", State: "running", Generation: 2, DocumentVersion: 3}
	if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error { return tx.Put("planning_run", run.ID, run) }); err != nil {
		t.Fatal(err)
	}
	return domain.LeaderExecutionRef{Kind: "planning", ID: run.ID, Generation: run.Generation, InputVersion: run.DocumentVersion}
}
func readActivity(t *testing.T, f *serviceFixture) domain.LeaderActivityPage {
	t.Helper()
	page, err := (LeaderActivities{f.s}).Read(context.Background(), f.owner, domain.LeaderActivityQuery{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	return page
}

func TestLeaderCaptureFencesRedactsBoundsAndIgnoresLateCallback(t *testing.T) {
	f := newServiceFixture(t)
	ref := activityRun(t, f)
	var late context.Context
	executor := planningExecFunc(func(ctx context.Context, _, _ string) (PlanningOutcome, error) {
		late = ctx
		ReportLeaderActivity(ctx, domain.LeaderActivitySignal{Key: "secret", Kind: "message", Text: "token=SECRET /Users/private/file"})
		for i := 0; i < 700; i++ {
			ReportLeaderActivity(ctx, domain.LeaderActivitySignal{Key: fmt.Sprint(i), Kind: "message", Text: strings.Repeat("字", 3000)})
		}
		ReportLeaderActivity(ctx, domain.LeaderActivitySignal{Key: "reasoning", Kind: "reasoning", Text: "PRIVATE_REASONING"})
		return PlanningOutcome{TreeStopped: true}, nil
	})
	out, err := executeObservedLeader(context.Background(), f.s, ref, executor, "", "", nil)
	if err != nil || !out.TreeStopped {
		t.Fatal(out, err)
	}
	ReportLeaderActivity(late, domain.LeaderActivitySignal{Key: "late", Kind: "message", Text: "LATE"})
	page := readActivity(t, f)
	if len(page.Events) == 0 {
		t.Fatal("no capture")
	}
	for _, event := range page.Events {
		if len(event.Text) > domain.MaxLeaderEventText || strings.Contains(event.Text, "SECRET") || strings.Contains(event.Text, "PRIVATE") || event.Text == "LATE" {
			t.Fatal("unbounded/private/late event", event.Kind)
		}
	}
	capture := &activityCapture{service: f.s, ref: ref}
	for _, change := range []domain.LeaderExecutionRef{
		{Kind: ref.Kind, ID: ref.ID, Generation: 1, InputVersion: 3}, {Kind: ref.Kind, ID: ref.ID, Generation: 2, InputVersion: 2},
	} {
		capture.ref = change
		capture.save([]domain.LeaderActivityEvent{{Execution: change, Key: "stale", Kind: "message", Text: "STALE"}})
	}
	for _, event := range readActivity(t, f).Events {
		if event.Text == "STALE" {
			t.Fatal("stale event accepted")
		}
	}
}

func TestLeaderActivityAuthorityAndFollowRevocation(t *testing.T) {
	f := newServiceFixture(t)
	ref := activityRun(t, f)
	chat, grant := chatFixture(t, f)
	activities := LeaderActivities{f.s}
	if _, err := activities.Read(context.Background(), f.node, domain.LeaderActivityQuery{Limit: 10}); err == nil {
		t.Fatal("node read Leader")
	}
	_, nodeGrant := nodeChatFixture(t, f, f.node, "node-chat")
	nodeChat, err := f.s.Authenticate(context.Background(), nodeGrant.Token)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := activities.Read(context.Background(), nodeChat, domain.LeaderActivityQuery{Limit: 10}); err == nil {
		t.Fatal("node chat read Leader")
	}
	received := make(chan domain.LeaderActivityPage, 4)
	finished := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go func() {
		finished <- activities.Follow(ctx, chat, domain.LeaderActivityQuery{Limit: 10}, func(page domain.LeaderActivityPage) error { received <- page; return nil })
	}()
	<-received
	capture := &activityCapture{service: f.s, ref: ref}
	capture.save([]domain.LeaderActivityEvent{{Execution: ref, Key: "wake", Kind: "message", Text: "public"}})
	select {
	case page := <-received:
		if len(page.Events) != 1 {
			t.Fatal(page)
		}
	case <-ctx.Done():
		t.Fatal("commit wake missed")
	}
	if _, err := f.s.RevokeChatCredential(ctx, f.owner, "revoke", grant.Credential.ID); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("revocation ignored")
		}
	case <-ctx.Done():
		t.Fatal("revocation did not close stream")
	}
}

func TestLeaderCaptureWorkerInitializationPath(t *testing.T) {
	s, owner, _ := initFixture(t)
	requestInit(t, s, owner, "existing")
	result := initResult(t, s, false)
	raw, _ := json.Marshal(result)
	worker := InitializationWorker{Service: s, Executor: planningExecFunc(func(ctx context.Context, _, _ string) (PlanningOutcome, error) {
		ReportLeaderActivity(ctx, domain.LeaderActivitySignal{Key: "progress", Kind: "message", Text: "正在检查模块"})
		return PlanningOutcome{Output: string(raw), TreeStopped: true}, nil
	})}
	if err := worker.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	page, err := (LeaderActivities{s}).Read(context.Background(), owner, domain.LeaderActivityQuery{Limit: 10})
	if err != nil || len(page.Events) != 1 || page.Executions[0].Ref.Kind != "initialization" {
		t.Fatal(page, err)
	}
}

func TestLeaderActivitySlowConsumerCannotBlockProducerOrRetainOtherChats(t *testing.T) {
	f := newServiceFixture(t)
	ref := activityRun(t, f)
	activities := LeaderActivities{f.s}
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		done <- activities.Follow(ctx, f.owner, domain.LeaderActivityQuery{Limit: 10}, func(domain.LeaderActivityPage) error {
			close(entered)
			<-release
			return context.Canceled
		})
	}()
	<-entered
	saved := make(chan struct{})
	go func() {
		capture := activityCapture{service: f.s, ref: ref}
		capture.save([]domain.LeaderActivityEvent{
			{Execution: ref, Key: "public", Kind: "message", Text: "safe"},
		})
		close(saved)
	}()
	select {
	case <-saved:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("slow consumer held database lock")
	}
	close(release)
	<-done
	chat, _ := chatFixture(t, f)
	private := domain.ConversationRequest{ID: "another-conversation", Source: domain.ChatSource{ClientID: "other-chat"}}
	if err := f.s.DB.Update(ctx, func(tx *store.Tx) error {
		var run domain.PlanningRun
		if err := tx.Get("planning_run", ref.ID, &run); err != nil {
			return err
		}
		run.Purpose, run.ConversationID = domain.ConsultationPurpose, private.ID
		if err := tx.Put("planning_run", ref.ID, run); err != nil {
			return err
		}
		return tx.Put("conversation", private.ID, private)
	}); err != nil {
		t.Fatal(err)
	}
	page, err := activities.Read(ctx, chat, domain.LeaderActivityQuery{Limit: 10})
	if err != nil || len(page.Events) != 0 || len(page.Executions) != 0 {
		t.Fatal("private conversation exposed", page, err)
	}
	if _, err := activities.Read(ctx, chat, domain.LeaderActivityQuery{Limit: 10, Kind: ref.Kind, ID: ref.ID}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("guessed private execution exposed", err)
	}
}
