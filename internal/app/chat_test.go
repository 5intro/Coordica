package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func chatFixture(t *testing.T, f *serviceFixture) (Actor, domain.ChatCredentialGrant) {
	t.Helper()
	b, e := f.s.CreateChatCredential(context.Background(), f.owner, "chat-client", domain.ChatCredentialRequest{Name: "Familiar chat"})
	grant := decodeTest[domain.ChatCredentialGrant](t, b, e)
	a, e := f.s.Authenticate(context.Background(), grant.Token)
	if e != nil {
		t.Fatal(e)
	}
	return a, grant
}
func consultationInput(a Actor, version int64) domain.ConversationInput {
	return domain.ConversationInput{ExpectedGoalVersion: version, OriginalText: "Should we change architecture? User asks for advice only.", AssistantSummary: "Architecture question", Source: domain.ChatSource{ClientID: a.ID, SessionID: "session_1", MessageID: "message_1"}}
}
func consultTest(t *testing.T, f *serviceFixture, a Actor, version int64, key, kind string) domain.ConversationRequest {
	t.Helper()
	in := consultationInput(a, version)
	in.Source.MessageID = key
	b, e := f.s.ConsultLeader(context.Background(), a, key, kind, in)
	return decodeTest[domain.ConversationRequest](t, b, e)
}
func getConversation(t *testing.T, f *serviceFixture, id string) domain.ConversationRequest {
	t.Helper()
	var q domain.ConversationRequest
	if e := f.s.DB.View(context.Background(), func(tx *store.Tx) error { return tx.Get("conversation", id, &q) }); e != nil {
		t.Fatal(e)
	}
	return q
}
func TestConsultationNeverDerivesOrChangesAuthority(t *testing.T) {
	for _, malicious := range []bool{false, true} {
		t.Run(map[bool]string{false: "reply", true: "malicious-actions"}[malicious], func(t *testing.T) {
			f, d, actions := planningFixture(t, true)
			a, _ := chatFixture(t, f)
			before := f.snapshot(t)
			q := consultTest(t, f, a, d.Version, "advice", "consultation")
			var output string
			if malicious {
				raw, _ := json.Marshal(actions)
				output = string(raw)
			} else {
				output = `{"reply":"An advisory answer only"}`
			}
			w := &PlanningWorker{Service: f.s, Executor: planningExecFunc(func(_ context.Context, dir, prompt string) (PlanningOutcome, error) {
				if !strings.HasPrefix(prompt, "COORDICA_ADVISORY_V1\n") || strings.Contains(prompt, "owner.token") || strings.Contains(prompt, "git status") {
					t.Error("not isolated advisory context")
				}
				if !strings.HasPrefix(filepath.Base(dir), "coordica-planning-") {
					t.Error("not isolated")
				}
				return PlanningOutcome{Output: output, TreeStopped: true, ElapsedSeconds: 2}, nil
			})}
			if e := w.Tick(context.Background()); e != nil {
				t.Fatal(e)
			}
			if e := w.Recover(context.Background()); e != nil {
				t.Fatal(e)
			}
			after := f.snapshot(t)
			if !reflect.DeepEqual(before.Tasks, after.Tasks) || !reflect.DeepEqual(before.PlanningDocument, after.PlanningDocument) || !reflect.DeepEqual(before.Decisions, after.Decisions) || !reflect.DeepEqual(before.ScopeRequests, after.ScopeRequests) || !reflect.DeepEqual(before.Runs, after.Runs) {
				t.Fatal("consultation changed execution authority")
			}
			got := getConversation(t, f, q.ID)
			if malicious {
				if got.Status != "failed" || got.Reply != "" {
					t.Fatal(got)
				}
			} else if got.Status != "succeeded" || got.Reply == "" || got.ResultSHA256 == "" || got.RepliedAt == nil {
				t.Fatal(got)
			}
			if after.Project.UsedSeconds != before.Project.UsedSeconds+2 || after.ConsultationAccount.UsedSeconds != 2 || after.ConsultationAccount.ReservedSeconds != 0 {
				t.Fatal("bad accounting")
			}
			// Even a corrupted/replayed pending result cannot call derivePlan.
			if e := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
				r := after.PlanningRuns[0]
				r.State = "pending_result"
				r.PendingResult = &actions
				return tx.Put("planning_run", r.ID, r)
			}); e != nil {
				t.Fatal(e)
			}
			if e := w.Recover(context.Background()); e != nil {
				t.Fatal(e)
			}
			if !reflect.DeepEqual(after.Tasks, f.snapshot(t).Tasks) {
				t.Fatal("recovery derived malicious consult")
			}
		})
	}
}
func TestConsultationAbsentGoalDraftAndProposalDoesNotStopDevelopment(t *testing.T) {
	for _, withDraft := range []bool{false, true} {
		t.Run(map[bool]string{false: "absent", true: "draft"}[withDraft], func(t *testing.T) {
			f := newServiceFixture(t)
			assignment := f.claim(t, "active-development")
			if assignment == nil {
				t.Fatal("fixture no active work")
			}
			a, _ := chatFixture(t, f)
			goalVer := int64(0)
			if withDraft {
				b, e := f.s.SavePlanning(context.Background(), f.owner, "draft", domain.PlanningSaveRequest{Document: domain.PlanningDocument{Objective: "Draft", BudgetSeconds: 1000}})
				d := decodeTest[domain.PlanningDocument](t, b, e)
				goalVer = d.Version
			}
			before := f.snapshot(t)
			q := consultTest(t, f, a, goalVer, "proposal", "proposal")
			if !reflect.DeepEqual(before.Tasks, f.snapshot(t).Tasks) || !reflect.DeepEqual(before.Runs, f.snapshot(t).Runs) {
				t.Fatal("proposal stopped active development")
			}
			w := &PlanningWorker{Service: f.s, Executor: planningExecFunc(func(context.Context, string, string) (PlanningOutcome, error) {
				return PlanningOutcome{TreeStopped: true, Output: `{"reply":"Keep draft for now"}`}, nil
			})}
			if e := w.Tick(context.Background()); e != nil {
				t.Fatal(e)
			}
			after := f.snapshot(t)
			if !reflect.DeepEqual(before.PlanningDocument, after.PlanningDocument) || !reflect.DeepEqual(before.Tasks, after.Tasks) {
				t.Fatal("consult synthesized or admitted goal")
			}
			if getConversation(t, f, q.ID).Status != "succeeded" {
				t.Fatal("draft consult failed")
			}
		})
	}
}
func TestChatRequirementIdempotenceSourceAndRevocationBeforeReplay(t *testing.T) {
	f := newServiceFixture(t)
	a, grant := chatFixture(t, f)
	ctx := context.Background()
	in := domain.ChatRequirementInput{Title: "Explicit user request", OriginalText: "User original\ntext exactly", AssistantSummary: "Assistant interpretation", Source: domain.ChatSource{SessionID: "session", MessageID: "message"}}
	before := f.snapshot(t)
	var wg sync.WaitGroup
	ids := make(chan string, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b, e := f.s.ChatSubmitRequirement(ctx, a, "requirement", in)
			if e != nil {
				t.Error(e)
				return
			}
			var r domain.Requirement
			if json.Unmarshal(b, &r) != nil {
				t.Error("invalid response")
			}
			ids <- r.ID
		}()
	}
	wg.Wait()
	close(ids)
	id := ""
	for got := range ids {
		if id != "" && id != got {
			t.Fatal("duplicate requirement")
		}
		id = got
	}
	after := f.snapshot(t)
	if len(after.Requirements) != len(before.Requirements)+1 || !reflect.DeepEqual(before.Tasks, after.Tasks) || after.PlanningDocument != nil {
		t.Fatal("requirement intake side effects")
	}
	r := after.Requirements[len(after.Requirements)-1]
	if r.Body != in.OriginalText || r.OriginalText != in.OriginalText || r.AssistantSummary != in.AssistantSummary || r.Source.ClientID != a.ID || r.Source.MessageID != "message" {
		t.Fatal("source/original lost", r)
	}
	in.Source.ClientID = "owner"
	if _, e := f.s.ChatSubmitRequirement(ctx, a, "forged-source", in); e == nil {
		t.Fatal("relay impersonated owner")
	}
	in.Source.ClientID = ""
	if _, e := f.s.RevokeChatCredential(ctx, f.owner, "revoke", grant.Credential.ID); e != nil {
		t.Fatal(e)
	}
	if _, e := f.s.ChatSubmitRequirement(ctx, a, "requirement", in); e == nil {
		t.Fatal("revoked replay returned success")
	}
	if _, e := f.s.Authenticate(ctx, grant.Token); e == nil {
		t.Fatal("revoked token authenticated")
	}
	if _, e := f.s.ChatList(ctx, a, domain.ChatListQuery{Kind: "tasks", Limit: 1}); e == nil {
		t.Fatal("stale actor read")
	}
	for _, actor := range []Actor{f.node, a} {
		if _, e := f.s.CreateChatCredential(ctx, actor, "unauthorized", domain.ChatCredentialRequest{Name: "X"}); e == nil {
			t.Fatal("non-owner created credential")
		}
	}
	if _, e := f.s.SchedulePlanning(ctx, a, "evaluate", domain.PlanningMessageRequest{}); e == nil {
		t.Fatal("chat evaluated")
	}
	if _, e := f.s.CreatePairing(ctx, a, "pairing", "X"); e == nil {
		t.Fatal("chat paired")
	}
}
func TestConsultationFiniteRetryReceiptReplayAndUnknownBarrier(t *testing.T) {
	f := newServiceFixture(t)
	a, _ := chatFixture(t, f)
	ctx := context.Background()
	q := consultTest(t, f, a, 0, "retry", "consultation")
	calls := 0
	w := &PlanningWorker{Service: f.s, Executor: planningExecFunc(func(context.Context, string, string) (PlanningOutcome, error) {
		calls++
		return PlanningOutcome{TreeStopped: true, ElapsedSeconds: 3}, errors.New("failure with secret")
	})}
	for i := 0; i < 4; i++ {
		if e := w.Tick(ctx); e != nil {
			t.Fatal(e)
		}
	}
	if calls != 3 || getConversation(t, f, q.ID).Status != "failed" {
		t.Fatal("unbounded/absent retry", calls)
	}
	before := f.snapshot(t)
	if before.ConsultationAccount.UsedSeconds != 9 || before.Project.ReservedSeconds != 0 {
		t.Fatal("retry budget")
	}
	for _, r := range before.PlanningRuns {
		raw, e := os.ReadFile(w.receipt(r.ID))
		if e != nil {
			t.Fatal(e)
		}
		var out PlanningOutcome
		if json.Unmarshal(raw, &out) != nil {
			t.Fatal("receipt")
		}
		if e = w.record(ctx, r, out); e != nil {
			t.Fatal(e)
		}
	}
	if f.snapshot(t).ConsultationAccount.UsedSeconds != 9 {
		t.Fatal("receipt replay double charged")
	}
	consultTest(t, f, a, 0, "unknown", "consultation")
	w.Executor = planningExecFunc(func(context.Context, string, string) (PlanningOutcome, error) {
		return PlanningOutcome{TreeStopped: false, Output: `{"reply":"Trust me, stopped"}`}, nil
	})
	if e := w.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	snap := f.snapshot(t)
	var r domain.PlanningRun
	for _, v := range snap.PlanningRuns {
		if v.State == "unknown_live" {
			r = v
		}
	}
	if r.ID == "" || snap.ConsultationAccount.UsedSeconds != 9+r.GrantSeconds || r.TreeStopped {
		t.Fatal("unknown refunded or invented stop")
	}
	if _, e := f.s.ConsultLeader(ctx, a, "blocked", "consultation", consultationInput(a, 0)); e == nil {
		t.Fatal("unknown did not block Leader")
	}
	if f.claim(t, "blocked-new-execution") != nil {
		t.Fatal("unknown allowed development")
	}
	if _, e := f.s.ConfirmConsultationStopped(ctx, a, "forbidden-stop", r.ID, domain.StopConfirmation{ExpectedVersion: r.Version, Evidence: "prose"}); e == nil {
		t.Fatal("chat proved stop")
	}
	if _, e := f.s.ConfirmConsultationStopped(ctx, f.owner, "stop", r.ID, domain.StopConfirmation{ExpectedVersion: r.Version, Evidence: "Operator proved process tree stopped"}); e != nil {
		t.Fatal(e)
	}
	if e := w.record(ctx, r, PlanningOutcome{Generation: r.Generation, DocumentVersion: 0, TreeStopped: false}); e != nil {
		t.Fatal(e)
	}
	if f.claim(t, "after-stop") == nil {
		t.Fatal("late duplicate reinstated unknown barrier")
	}
}
func TestConsultationCancelRevokePauseGoalChangesAndAccountingTarget(t *testing.T) {
	for _, action := range []string{"cancel", "revoke", "pause", "new-goal"} {
		t.Run(action, func(t *testing.T) {
			f := newServiceFixture(t)
			a, grant := chatFixture(t, f)
			ctx := context.Background()
			q := consultTest(t, f, a, 0, "consult", "consultation")
			w := &PlanningWorker{Service: f.s}
			r, _, _, e := w.reserve(ctx)
			if e != nil || r.ID == "" {
				t.Fatal(e)
			}
			switch action {
			case "node_scope", "legacy_scope":
				if e := f.s.DB.Update(ctx, func(tx *store.Tx) error {
					var c domain.Credential
					if e := tx.Get("credential", Hash(grant.Token), &c); e != nil {
						return e
					}
					c.ChatScope = ""
					if action == "node_scope" {
						c.ChatScope = domain.ChatScopeNode
						c.NodeID = f.node.NodeID
					}
					return tx.Put("credential", c.Hash, c)
				}); e != nil {
					t.Fatal(e)
				}
			case "cancel":
				b, e := f.s.CancelConversation(ctx, a, "cancel", domain.ChatCancelRequest{ID: q.ID, ExpectedVersion: getConversation(t, f, q.ID).Version})
				decodeTest[domain.ConversationRequest](t, b, e)
			case "revoke":
				if _, e := f.s.RevokeChatCredential(ctx, f.owner, "revoke", grant.Credential.ID); e != nil {
					t.Fatal(e)
				}
			case "pause":
				p := f.snapshot(t).Project
				if _, e := f.s.ProjectAction(ctx, f.owner, "pause", "pause", domain.VersionRequest{ExpectedVersion: p.Version}); e != nil {
					t.Fatal(e)
				}
			case "new-goal":
				if _, e := f.s.SavePlanning(ctx, f.owner, "new-goal", domain.PlanningSaveRequest{Document: domain.PlanningDocument{Objective: "New goal", BudgetSeconds: 1000}}); e != nil {
					t.Fatal(e)
				}
			}
			if e := w.record(ctx, r, PlanningOutcome{Generation: r.Generation, DocumentVersion: 0, FinishedAt: f.now.Add(time.Second), TreeStopped: true, ElapsedSeconds: 2, Output: `{"reply":"late reply"}`}); e != nil {
				t.Fatal(e)
			}
			snap := f.snapshot(t)
			if getConversation(t, f, q.ID).Reply != "" || getConversation(t, f, q.ID).Status != "stopped" {
				t.Fatal("canceled advisory accepted")
			}
			if snap.ConsultationAccount.ReservedSeconds != 0 || snap.ConsultationAccount.UsedSeconds != 2 || snap.Project.ReservedSeconds != 0 {
				t.Fatal("wrong account settlement")
			}
			if snap.PlanningDocument != nil && (snap.PlanningDocument.UsedSeconds != 0 || snap.PlanningDocument.ReservedSeconds != 0) {
				t.Fatal("new goal charged for old consultation")
			}
		})
	}
}
func TestConsultationRestartDurableReceiptAndBackupPrivacy(t *testing.T) {
	for _, receipt := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing-receipt", true: "receipt"}[receipt], func(t *testing.T) {
			f := newServiceFixture(t)
			a, grant := chatFixture(t, f)
			ctx := context.Background()
			q := consultTest(t, f, a, 0, "consult", "consultation")
			w := &PlanningWorker{Service: f.s}
			r, _, _, e := w.reserve(ctx)
			if e != nil {
				t.Fatal(e)
			}
			if receipt {
				if e = os.MkdirAll(filepath.Dir(w.receipt(r.ID)), 0700); e != nil {
					t.Fatal(e)
				}
				out := PlanningOutcome{Generation: r.Generation, DocumentVersion: 0, FinishedAt: f.now.Add(time.Second), Output: `{"reply":"Recovered advice"}`, TreeStopped: true, ElapsedSeconds: 2}
				bindPlanningOutcome(&out, r)
				raw, _ := json.Marshal(out)
				if e = os.WriteFile(w.receipt(r.ID), raw, 0600); e != nil {
					t.Fatal(e)
				}
			}
			// New service/worker instance over the durable database represents restart.
			restarted := New(f.s.DB, f.s.Dir)
			restarted.Now = f.s.Now
			w.Service = restarted
			if e = w.Recover(ctx); e != nil {
				t.Fatal(e)
			}
			got := getConversation(t, f, q.ID)
			if receipt {
				if got.Status != "succeeded" || got.Reply != "Recovered advice" {
					t.Fatal(got)
				}
			} else if got.Status != "unknown_live" || f.snapshot(t).ConsultationAccount.UsedSeconds != r.GrantSeconds {
				t.Fatal("restart refunded grant or guessed stop")
			}
			if e = w.Recover(ctx); e != nil {
				t.Fatal(e)
			}
			backup, e := f.s.Backup(ctx, f.owner)
			if e != nil {
				t.Fatal(e)
			}
			raw, e := os.ReadFile(filepath.Join(backup, "coordica.db"))
			if e != nil {
				t.Fatal(e)
			}
			if strings.Contains(string(raw), grant.Token) || strings.Contains(string(raw), Hash(grant.Token)) {
				t.Fatal("credential or cached grant leaked to backup")
			}
			if receipt {
				if _, e = os.Stat(filepath.Join(backup, "planning-outcomes", r.ID+".json")); e != nil {
					t.Fatal("advisory receipt not backed up")
				}
			}
		})
	}
}
func TestChatBoundedRedactedQueriesAndOwnCancellation(t *testing.T) {
	f := newServiceFixture(t)
	a, _ := chatFixture(t, f)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		b, e := f.s.CreateRequirement(ctx, f.owner, ID("key_"), domain.Requirement{Title: "Do work", Body: "/Users/private/settings.json token=fixture-secret\n$ git -c credential.helper=secret"})
		decodeTest[domain.Requirement](t, b, e)
	}
	page, e := f.s.ChatList(ctx, a, domain.ChatListQuery{Kind: "requirements", Limit: 2})
	if e != nil || len(page.Items) != 2 || page.NextCursor == "" {
		t.Fatal(page, e)
	}
	raw, _ := json.Marshal(page)
	for _, secret := range []string{"/Users/private", "fixture-secret", "credential.helper"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("raw data leak", string(raw))
		}
	}
	next, e := f.s.ChatList(ctx, a, domain.ChatListQuery{Kind: "requirements", Limit: 2, Cursor: page.NextCursor})
	if e != nil || len(next.Items) != 1 || next.NextCursor != "" || next.Items[0].ID == page.Items[0].ID {
		t.Fatal(next, e)
	}
	for _, query := range []domain.ChatListQuery{{Kind: "unknown", Limit: 1}, {Kind: "tasks", Limit: 100}, {Kind: "tasks", Limit: 0}, {Kind: "tasks", Limit: 1, Cursor: "../../owner.token"}} {
		if _, e = f.s.ChatList(ctx, a, query); e == nil {
			t.Fatal("unbounded/open query")
		}
	}
	q := consultTest(t, f, a, 0, "advice", "consultation")
	b, e := f.s.CreateChatCredential(ctx, f.owner, "second", domain.ChatCredentialRequest{Name: "Second"})
	second := decodeTest[domain.ChatCredentialGrant](t, b, e)
	other, e := f.s.Authenticate(ctx, second.Token)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.CancelConversation(ctx, other, "cancel-other", domain.ChatCancelRequest{ID: q.ID, ExpectedVersion: q.Version}); e == nil {
		t.Fatal("other client canceled request")
	}
	if _, e = f.s.ConsultLeader(ctx, a, "stale-goal", "proposal", consultationInput(a, 9)); e == nil {
		t.Fatal("stale version accepted")
	}
}

func TestConsultationLiveCancellationAndTimeoutSupervisor(t *testing.T) {
	for _, action := range []string{"cancel", "revoke", "pause", "timeout", "node_scope", "legacy_scope"} {
		t.Run(action, func(t *testing.T) {
			f := newServiceFixture(t)
			f.s.Now = func() time.Time { return time.Now().UTC() }
			a, grant := chatFixture(t, f)
			ctx := context.Background()
			if action == "timeout" {
				settings := f.snapshot(t).Automation
				settings.PlanningTimeoutSeconds = 1
				settings.PlanningBudgetSeconds = 1
				settings.PlanningMaxRetries = 0
				if _, e := f.s.ConfigureAutomation(ctx, f.owner, "short", domain.AutomationRequest{ExpectedVersion: settings.Version, Settings: settings}); e != nil {
					t.Fatal(e)
				}
			}
			q := consultTest(t, f, a, 0, "live", "consultation")
			started := make(chan struct{})
			done := make(chan error, 1)
			w := &PlanningWorker{Service: f.s, Executor: planningExecFunc(func(execCtx context.Context, _ string, _ string) (PlanningOutcome, error) {
				close(started)
				<-execCtx.Done()
				return PlanningOutcome{TreeStopped: true}, execCtx.Err()
			})}
			go func() { done <- w.Tick(ctx) }()
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("not started")
			}
			switch action {
			case "node_scope", "legacy_scope":
				if e := f.s.DB.Update(ctx, func(tx *store.Tx) error {
					var c domain.Credential
					if e := tx.Get("credential", Hash(grant.Token), &c); e != nil {
						return e
					}
					c.ChatScope = ""
					if action == "node_scope" {
						c.ChatScope = domain.ChatScopeNode
						c.NodeID = f.node.NodeID
					}
					return tx.Put("credential", c.Hash, c)
				}); e != nil {
					t.Fatal(e)
				}
			case "cancel":
				current := getConversation(t, f, q.ID)
				if _, e := f.s.CancelConversation(ctx, a, "cancel", domain.ChatCancelRequest{ID: q.ID, ExpectedVersion: current.Version}); e != nil {
					t.Fatal(e)
				}
			case "revoke":
				if _, e := f.s.RevokeChatCredential(ctx, f.owner, "revoke", grant.Credential.ID); e != nil {
					t.Fatal(e)
				}
			case "pause":
				p := f.snapshot(t).Project
				if _, e := f.s.ProjectAction(ctx, f.owner, "pause", "pause", domain.VersionRequest{ExpectedVersion: p.Version}); e != nil {
					t.Fatal(e)
				}
			}
			select {
			case e := <-done:
				if e != nil {
					t.Fatal(e)
				}
			case <-time.After(4 * time.Second):
				t.Fatal("supervisor did not cancel finite execution")
			}
			snap := f.snapshot(t)
			got := getConversation(t, f, q.ID)
			if got.Status != "stopped" || got.Reply != "" || snap.Project.ReservedSeconds != 0 || !snap.PlanningRuns[0].TreeStopped {
				t.Fatal("unproved stop or reply after cancel", got)
			}
			if action == "timeout" && snap.ConsultationAccount.UsedSeconds != 1 {
				t.Fatal("timeout refunded finite grant")
			}
		})
	}
}
func TestConsultationIgnoresLegacyProjectCapAndPreservesGenerationFence(t *testing.T) {
	f := newServiceFixture(t)
	a, _ := chatFixture(t, f)
	ctx := context.Background()
	if e := f.s.DB.Update(ctx, func(tx *store.Tx) error {
		p, e := getProject(tx)
		if e != nil {
			return e
		}
		p.TotalBudgetSeconds = 5
		p.UsedSeconds = 50
		return tx.Put("project", p.ID, p)
	}); e != nil {
		t.Fatal(e)
	}
	if _, e := f.s.ProjectBudget(ctx, f.owner, "deprecated", domain.BudgetRequest{}); e == nil {
		t.Fatal("legacy cap endpoint implied an allowance")
	}

	q := consultTest(t, f, a, 0, "advice", "consultation")
	w := &PlanningWorker{Service: f.s}
	r, _, _, e := w.reserve(ctx)
	if e != nil || r.GrantSeconds != f.snapshot(t).Automation.PlanningTimeoutSeconds {
		t.Fatal("grant should follow per-consultation timeout", r, e)
	}
	if e = w.record(ctx, r, PlanningOutcome{Generation: r.Generation + 1, DocumentVersion: r.DocumentVersion, TreeStopped: true, Output: `{"reply":"wrong generation"}`}); e == nil {
		t.Fatal("generation mismatch accepted")
	}
	if f.snapshot(t).Project.ReservedSeconds != r.GrantSeconds || getConversation(t, f, q.ID).Reply != "" {
		t.Fatal("stale receipt changed accounting")
	}
	if e = w.record(ctx, r, PlanningOutcome{Generation: r.Generation, DocumentVersion: r.DocumentVersion, TreeStopped: false, ElapsedSeconds: 1}); e != nil {
		t.Fatal(e)
	}
	if f.snapshot(t).Project.UsedSeconds != 50+r.GrantSeconds {
		t.Fatal("unknown not fully charged")
	}
}

func TestConsultationDoesNotDrivePendingScopeOwnerDecisions(t *testing.T) {
	f, w, assignment, _ := scopeFixture(t)
	ctx := context.Background()
	f.now = assignment.Run.LeaseUntil.Add(time.Second)
	if e := f.s.Expire(ctx); e != nil {
		t.Fatal(e)
	}
	a, _ := chatFixture(t, f)
	before := f.snapshot(t)
	consultTest(t, f, a, before.PlanningDocument.Version, "pure-advice", "consultation")
	w.Executor = planningExecFunc(func(context.Context, string, string) (PlanningOutcome, error) {
		return PlanningOutcome{TreeStopped: true, Output: `{"reply":"Read-only advice while waiting for stop proof"}`}, nil
	})
	if e := w.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	after := f.snapshot(t)
	if !reflect.DeepEqual(before.ScopeRequests, after.ScopeRequests) || !reflect.DeepEqual(before.Decisions, after.Decisions) || !reflect.DeepEqual(before.Tasks, after.Tasks) || !reflect.DeepEqual(before.Runs, after.Runs) {
		t.Fatal("advisory preflight drove scope or Owner decisions")
	}
}

func TestConsultationRestoreRetainsAccountAndRecordsWithoutReusingClientAuthority(t *testing.T) {
	f := newServiceFixture(t)
	a, grant := chatFixture(t, f)
	ctx := context.Background()
	q := consultTest(t, f, a, 0, "restore-consult", "consultation")
	w := &PlanningWorker{Service: f.s}
	r, _, _, e := w.reserve(ctx)
	if e != nil {
		t.Fatal(e)
	}
	backup, e := f.s.Backup(ctx, f.owner)
	if e != nil {
		t.Fatal(e)
	}
	dest := filepath.Join(t.TempDir(), "restored")
	if e = RestoreBackup(ctx, backup, dest); e != nil {
		t.Fatal(e)
	}
	db, e := store.Open(filepath.Join(dest, "coordica.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	restored := New(db, dest)
	if _, e = restored.Authenticate(ctx, grant.Token); e == nil {
		t.Fatal("restored chat authority reused")
	}
	recovery := &PlanningWorker{Service: restored}
	if e = recovery.Recover(ctx); e != nil {
		t.Fatal(e)
	}
	snap, e := restored.Snapshot(ctx, f.owner)
	if e != nil {
		t.Fatal(e)
	}
	if !snap.Project.Paused || snap.PlanningDocument != nil || snap.Project.ReservedSeconds != 0 || snap.Project.UsedSeconds != r.GrantSeconds || snap.ConsultationAccount.UsedSeconds != r.GrantSeconds || snap.ConsultationAccount.ReservedSeconds != 0 {
		t.Fatal("restored wrong account or guessed stop", snap.Project, snap.ConsultationAccount)
	}
	if len(snap.Conversations) != 1 || snap.Conversations[0].ID != q.ID || snap.Conversations[0].Source.ClientID != grant.Credential.ID || snap.Conversations[0].OriginalText != q.OriginalText || snap.Conversations[0].Status != "unknown_live" {
		t.Fatal("restore lost source/evidence", snap.Conversations)
	}
}

func TestChatLargeAdvisoryPagesStopAtByteBoundWithoutLosingCursorItems(t *testing.T) {
	f := newServiceFixture(t)
	a, _ := chatFixture(t, f)
	ctx := context.Background()
	w := &PlanningWorker{Service: f.s, Executor: planningExecFunc(func(context.Context, string, string) (PlanningOutcome, error) {
		raw, _ := json.Marshal(domain.ConsultResult{Reply: strings.Repeat("r", 16<<10)})
		return PlanningOutcome{Output: string(raw), TreeStopped: true}, nil
	})}
	for i := 0; i < 24; i++ {
		in := consultationInput(a, 0)
		in.Source.MessageID = ID("message_")
		in.OriginalText = strings.Repeat("u", 16<<10)
		in.AssistantSummary = strings.Repeat("s", 4<<10)
		if _, e := f.s.ConsultLeader(ctx, a, ID("large_"), "consultation", in); e != nil {
			t.Fatal(e)
		}
		if e := w.Tick(ctx); e != nil {
			t.Fatal(e)
		}
	}
	cursor := ""
	seen := map[string]bool{}
	shortPage := false
	for pages := 0; pages < 4; pages++ {
		page, e := f.s.ChatList(ctx, a, domain.ChatListQuery{Kind: "consultations", Cursor: cursor, Limit: 20})
		if e != nil {
			t.Fatal(e)
		}
		size := 0
		for _, item := range page.Items {
			raw, _ := json.Marshal(item)
			size += len(raw)
			if seen[item.ID] {
				t.Fatal("duplicate cursor item")
			}
			seen[item.ID] = true
		}
		if size > 1<<20 {
			t.Fatal("unbounded response", size)
		}
		if page.NextCursor != "" && len(page.Items) < 20 {
			shortPage = true
		}
		cursor = page.NextCursor
		if cursor == "" {
			break
		}
	}
	if len(seen) != 24 || !shortPage {
		t.Fatal("byte cutoff lost records or did not apply", len(seen), shortPage)
	}
}

func TestChatSourceDuplicateMessagesDoNotCreateNewSubmissions(t *testing.T) {
	f := newServiceFixture(t)
	a, grant := chatFixture(t, f)
	ctx := context.Background()
	in := domain.ChatRequirementInput{Title: "User requirement", OriginalText: "Original text", AssistantSummary: "Separate summary", Source: domain.ChatSource{SessionID: "session", MessageID: "message"}}
	first, e := f.s.ChatSubmitRequirement(ctx, a, "first-key", in)
	if e != nil {
		t.Fatal(e)
	}
	in.Source.ClientID = a.ID
	second, e := f.s.ChatSubmitRequirement(ctx, a, "new-transport-key", in)
	if e != nil || string(second) != string(first) || len(f.snapshot(t).Requirements) != 1 {
		t.Fatal("duplicate source created requirement", e)
	}
	in.AssistantSummary = "Different interpretation"
	if _, e = f.s.ChatSubmitRequirement(ctx, a, "conflicting-source", in); e == nil {
		t.Fatal("source silently rewritten")
	}
	advice := consultationInput(a, 0)
	b, e := f.s.ConsultLeader(ctx, a, "consult-first", "consultation", advice)
	q := decodeTest[domain.ConversationRequest](t, b, e)
	b, e = f.s.ConsultLeader(ctx, a, "consult-retry", "consultation", advice)
	same := decodeTest[domain.ConversationRequest](t, b, e)
	if q.ID != same.ID || len(f.snapshot(t).PlanningRuns) != 1 {
		t.Fatal("duplicate source queued another Leader")
	}
	if _, e = f.s.RevokeChatCredential(ctx, f.owner, "revoke", grant.Credential.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.ConsultLeader(ctx, a, "new-key-after-revoke", "consultation", advice); e == nil {
		t.Fatal("source replay bypassed revocation")
	}
}
