package app

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func nodeChatFixture(t *testing.T, f *serviceFixture, node Actor, key string) (Actor, domain.ChatCredentialGrant) {
	t.Helper()
	raw, e := f.s.CreateNodeChatCredential(context.Background(), node, key, domain.ChatCredentialRequest{Name: "Shared node host"})
	g := decodeTest[domain.ChatCredentialGrant](t, raw, e)
	a, e := f.s.Authenticate(context.Background(), g.Token)
	if e != nil {
		t.Fatal(e)
	}
	return a, g
}
func anotherNode(t *testing.T, f *serviceFixture) Actor {
	t.Helper()
	ctx := context.Background()
	raw, e := f.s.CreatePairing(ctx, f.owner, "second-node-pair", "Other")
	p := decodeTest[map[string]any](t, raw, e)
	n, e := f.s.Register(ctx, domain.RegisterRequest{Code: p["code"].(string), Name: "Other", Platform: "test", Capacity: 1})
	if e != nil {
		t.Fatal(e)
	}
	a, e := f.s.Authenticate(ctx, n.Token)
	if e != nil {
		t.Fatal(e)
	}
	return a
}
func TestCenterChatPrivateListGuessingAndReplyChains(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	a, _ := chatFixture(t, f)
	raw, e := f.s.CreateChatCredential(ctx, f.owner, "other-chat", domain.ChatCredentialRequest{Name: "Other chat"})
	grant := decodeTest[domain.ChatCredentialGrant](t, raw, e)
	other, e := f.s.Authenticate(ctx, grant.Token)
	if e != nil {
		t.Fatal(e)
	}
	q := consultTest(t, f, a, 0, "private-question", "consultation")
	w := &PlanningWorker{Service: f.s, Executor: planningExecFunc(func(context.Context, string, string) (PlanningOutcome, error) {
		return PlanningOutcome{TreeStopped: true, Output: `{"reply":"private answer"}`}, nil
	})}
	if e = w.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	page, e := f.s.ChatList(ctx, other, domain.ChatListQuery{Kind: "consultations", Limit: 20})
	if e != nil || len(page.Items) != 0 {
		t.Fatal("private conversation exposed", page, e)
	}
	for _, id := range []string{q.ID, "consult_guessed"} {
		if _, e = f.s.ChatItem(ctx, other, domain.ChatItemQuery{Kind: "consultations", ID: id}); !errors.Is(e, domain.ErrNotFound) {
			t.Fatal("guess distinguished private request", e)
		}
		before := f.snapshot(t)
		input := consultationInput(other, 0)
		input.ReplyTo = id
		if _, e = f.s.ConsultLeader(ctx, other, "foreign-reply-"+id, "consultation", input); !errors.Is(e, domain.ErrNotFound) {
			t.Fatal("foreign reply accepted or revealed", e)
		}
		after := f.snapshot(t)
		if !reflect.DeepEqual(before, after) {
			t.Fatal("rejected reply changed account/request/audit")
		}
	}
	input := consultationInput(a, 0)
	input.ReplyTo = q.ID
	input.Source.MessageID = "new-turn"
	input.Source.SessionID = "foreign-session"
	if _, e = f.s.ConsultLeader(ctx, a, "wrong-session", "consultation", input); !errors.Is(e, domain.ErrNotFound) {
		t.Fatal("cross session reply", e)
	}
	input.Source.SessionID = q.Source.SessionID
	input.ExpectedGoalVersion = 7
	if _, e = f.s.ConsultLeader(ctx, a, "wrong-version", "consultation", input); !errors.Is(e, domain.ErrNotFound) {
		t.Fatal("incompatible goal reply", e)
	}
	input.ExpectedGoalVersion = 0
	input.OriginalText = "follow up to the answer"
	raw, e = f.s.ConsultLeader(ctx, a, "follow-up", "consultation", input)
	next := decodeTest[domain.ConversationRequest](t, raw, e)
	run, _, _, e := w.reserve(ctx)
	if e != nil || run.ConversationID != next.ID {
		t.Fatal(run, e)
	}
	prompt, e := w.consultationPrompt(ctx, run)
	if e != nil {
		t.Fatal(e)
	}
	for _, text := range []string{input.OriginalText, q.OriginalText, "private answer"} {
		if !strings.Contains(prompt, text) {
			t.Fatal("missing authorized ancestral context", text)
		}
	}
	if e = w.record(ctx, run, PlanningOutcome{Generation: run.Generation, DocumentVersion: run.DocumentVersion, TreeStopped: true, Output: `{"reply":"second-turn answer"}`}); e != nil {
		t.Fatal(e)
	}
	input.ReplyTo = next.ID
	input.Source.MessageID = "third-turn"
	input.OriginalText = "third question with context"
	if _, e = f.s.ConsultLeader(ctx, a, "third-turn", "consultation", input); e != nil {
		t.Fatal(e)
	}
	run, _, _, e = w.reserve(ctx)
	if e != nil {
		t.Fatal(e)
	}
	prompt, e = w.consultationPrompt(ctx, run)
	if e != nil {
		t.Fatal(e)
	}
	for _, text := range []string{"third question with context", "follow up to the answer", "second-turn answer", q.OriginalText, "private answer"} {
		if !strings.Contains(prompt, text) {
			t.Fatal("multi-turn ancestry lost", text)
		}
	}
}
func TestNodeChatAuthorityAndReassignedHistoricalRunIsolation(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	center, _ := chatFixture(t, f)
	nodeA, grantA := nodeChatFixture(t, f, f.node, "grant-a")
	nodeB := anotherNode(t, f)
	chatB, grantB := nodeChatFixture(t, f, nodeB, "grant-b")
	for _, a := range []Actor{nodeA, chatB} {
		for _, kind := range []string{"consultation", "proposal"} {
			if _, e := f.s.ConsultLeader(ctx, a, "forbidden-"+kind, kind, consultationInput(a, 0)); e == nil {
				t.Fatal("node contacted Leader")
			}
		}
		if _, e := f.s.ChatList(ctx, a, domain.ChatListQuery{Kind: "consultations", Limit: 1}); e == nil {
			t.Fatal("node polled Leader")
		}
	}
	q := consultTest(t, f, center, 0, "center-only", "consultation")
	if _, e := f.s.CancelConversation(ctx, nodeA, "cancel-center", domain.ChatCancelRequest{ID: q.ID, ExpectedVersion: 1}); e == nil {
		t.Fatal("node canceled center work")
	}
	input := domain.ChatRequirementInput{Title: "Need a feature", OriginalText: "Keep src/api/routes.go", Source: domain.ChatSource{SessionID: "local", MessageID: "requirement"}}
	forged := nodeA
	forged.NodeID = nodeB.NodeID
	forged.ChatScope = domain.ChatScopeCenter
	raw, e := f.s.ChatSubmitRequirement(ctx, forged, "requirement", input)
	req := decodeTest[domain.Requirement](t, raw, e)
	if req.SourceNodeID != f.node.NodeID || req.Source.ClientID != nodeA.ID {
		t.Fatal("caller overrode server provenance", req)
	}
	assignment := f.claim(t, "owned-run")
	if assignment == nil {
		t.Fatal("no run")
	}
	runB := assignment.Run
	runB.ID = "run_other_node"
	runB.NodeID = nodeB.NodeID
	runB.State = "lost"
	runB.Quiescent = false
	runB.Generation++
	if e = f.s.DB.Update(ctx, func(tx *store.Tx) error {
		var task domain.Task
		if e := tx.Get("task", f.task.ID, &task); e != nil {
			return e
		}
		task.Title = "newer private assignment"
		task.Description = "private newer task"
		task.CurrentRunID = runB.ID
		task.Generation = runB.Generation
		if e := tx.Put("run", runB.ID, runB); e != nil {
			return e
		}
		return tx.Put("task", task.ID, task)
	}); e != nil {
		t.Fatal(e)
	}
	page, e := f.s.ChatList(ctx, forged, domain.ChatListQuery{Kind: "runs", Limit: 20})
	if e != nil || len(page.Items) != 1 || page.Items[0].ID != assignment.Run.ID {
		t.Fatal("forged node exposed other-node Run", page, e)
	}
	raw, _ = json.Marshal(page)
	if strings.Contains(string(raw), "private newer") || strings.Contains(string(raw), "newer private") || strings.Contains(string(raw), "session_id") || strings.Contains(string(raw), "base") || strings.Contains(string(raw), "candidate") {
		t.Fatal("unsafe historical run projection", string(raw))
	}
	if _, e = f.s.ChatItem(ctx, nodeA, domain.ChatItemQuery{Kind: "runs", ID: runB.ID}); !errors.Is(e, domain.ErrNotFound) {
		t.Fatal("guessed other node run", e)
	}
	for _, kind := range []string{"tasks", "blocks", "consultations", "proposals"} {
		if _, e = f.s.ChatList(ctx, forged, domain.ChatListQuery{Kind: kind, Limit: 20}); e == nil || !strings.Contains(e.Error(), "center interaction required") {
			t.Fatal("node listed center-only kind", kind, e)
		}
		for _, id := range []string{f.task.ID, runB.ID, "decision_guessed", "plan_guessed"} {
			if _, e = f.s.ChatItem(ctx, forged, domain.ChatItemQuery{Kind: kind, ID: id}); e == nil || !strings.Contains(e.Error(), "center interaction required") {
				t.Fatal("node guessed center-only detail", kind, id, e)
			}
		}
	}
	agents, e := f.s.ChatList(ctx, nodeA, domain.ChatListQuery{Kind: "agents", Limit: 20})
	if e != nil || len(agents.Items) != 1 || agents.Items[0].ID != f.node.NodeID {
		t.Fatal("other host agent exposed", agents, e)
	}
	if _, e = f.s.RevokeChatCredential(ctx, nodeB, "foreign-revoke", grantA.Credential.ID); !errors.Is(e, domain.ErrNotFound) {
		t.Fatal("node revoked foreign grant", e)
	}
	if _, e = f.s.RevokeChatCredential(ctx, f.node, "own-revoke", grantA.Credential.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.CreateNodeChatCredential(ctx, f.node, "grant-a", domain.ChatCredentialRequest{Name: "Shared node host"}); e == nil {
		t.Fatal("revoked grant replay returned token")
	}
	if _, e = f.s.ChatSubmitRequirement(ctx, nodeA, "requirement", input); e == nil {
		t.Fatal("revoked requirement replay")
	}
	if _, e = f.s.RevokeNode(ctx, f.owner, "revoke-host", nodeB.NodeID); e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.Authenticate(ctx, grantB.Token); e == nil {
		t.Fatal("revoked host retained chat authentication")
	}
	if _, e = f.s.ChatOverview(ctx, chatB); e == nil {
		t.Fatal("stale chat Actor bypassed live node registration")
	}
	if _, e = f.s.CreateNodeChatCredential(ctx, nodeB, "grant-b", domain.ChatCredentialRequest{Name: "Shared node host"}); e == nil {
		t.Fatal("revoked node replayed grant")
	}
}
func TestLegacyUnscopedChatCannotAuthenticateOrResumeQueuedLeader(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	a, g := chatFixture(t, f)
	q := consultTest(t, f, a, 0, "legacy", "consultation")
	if e := f.s.DB.Update(ctx, func(tx *store.Tx) error {
		var c domain.Credential
		if e := tx.Get("credential", Hash(g.Token), &c); e != nil {
			return e
		}
		c.ChatScope = ""
		return tx.Put("credential", c.Hash, c)
	}); e != nil {
		t.Fatal(e)
	}
	if _, e := f.s.Authenticate(ctx, g.Token); e == nil {
		t.Fatal("legacy credential inferred center authority")
	}
	w := &PlanningWorker{Service: f.s}
	r, _, _, e := w.reserve(ctx)
	if e != nil || r.ID != "" {
		t.Fatal("legacy work resumed", r, e)
	}
	if got := getConversation(t, f, q.ID); got.Status != "stale" {
		t.Fatal(got)
	}
}
