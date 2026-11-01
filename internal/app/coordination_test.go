package app

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func workContextTest(t *testing.T, f *serviceFixture, a Actor, anchor domain.WorkAnchor, refs ...domain.WorkReference) domain.WorkContext {
	t.Helper()
	v, err := (Coordination{f.s}).Context(context.Background(), a, domain.WorkContextQuery{Anchor: anchor, Limit: 20, References: refs})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func workInputTest(t *testing.T, f *serviceFixture, a Actor, anchor domain.WorkAnchor, message string) domain.WorkNoteInput {
	t.Helper()
	v := workContextTest(t, f, a, anchor)
	return domain.WorkNoteInput{Anchor: anchor, ExpectedStamp: v.Current.Reference.Stamp, Kind: "note", OriginalText: "Discuss only; change acceptance later through the formal workflow.", References: []domain.WorkReference{}, Source: domain.ChatSource{SessionID: "session_1", MessageID: message}}
}

func recordWorkTest(t *testing.T, f *serviceFixture, a Actor, key string, in domain.WorkNoteInput) domain.WorkNoteReceipt {
	t.Helper()
	b, err := (Coordination{f.s}).Record(context.Background(), a, key, in)
	return decodeTest[domain.WorkNoteReceipt](t, b, err)
}

func TestWorkNoteConcurrentReplayCrossSessionPrivacyAndRevocation(t *testing.T) {
	f := newServiceFixture(t)
	a, grant := chatFixture(t, f)
	anchor := domain.WorkAnchor{Kind: "task", ID: f.task.ID}
	in := workInputTest(t, f, a, anchor, "message")
	in.OriginalText = "User original\n/Users/private/owner.token token=sample-secret"
	before := f.snapshot(t)
	var wg sync.WaitGroup
	ids := make(chan string, 6)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b, e := (Coordination{f.s}).Record(context.Background(), a, "stable", in)
			if e != nil {
				t.Error(e)
				return
			}
			var v domain.WorkNoteReceipt
			if e = json.Unmarshal(b, &v); e != nil {
				t.Error(e)
				return
			}
			ids <- v.Note.ID
		}()
	}
	wg.Wait()
	close(ids)
	id := ""
	for got := range ids {
		if id != "" && id != got {
			t.Fatal("duplicate note")
		}
		id = got
	}
	v := recordWorkTest(t, f, a, "different-transport-key", in)
	if v.Note.ID != id || !v.Note.Redacted || v.Note.OriginalTextSHA256 != Hash(in.OriginalText) || strings.Contains(v.Note.OriginalText, "sample-secret") || v.Disposition != "recorded_only" {
		t.Fatal(v)
	}
	ownerView := workContextTest(t, f, f.owner, anchor)
	if len(ownerView.Notes) != 1 || ownerView.Notes[0].OriginalText != in.OriginalText || ownerView.Notes[0].Source.ClientID != a.ID {
		t.Fatal("original/source lost", ownerView.Notes)
	}
	// Session IDs establish provenance, not a read partition for this credential.
	in.Source.SessionID = "session_2"
	in.Source.MessageID = "next-message"
	_ = recordWorkTest(t, f, a, "next-session", in)
	if len(workContextTest(t, f, a, anchor).Notes) != 2 {
		t.Fatal("cross-session continuity lost")
	}
	otherRaw, e := f.s.CreateChatCredential(context.Background(), f.owner, "other-chat", domain.ChatCredentialRequest{Name: "Other"})
	otherGrant := decodeTest[domain.ChatCredentialGrant](t, otherRaw, e)
	other, e := f.s.Authenticate(context.Background(), otherGrant.Token)
	if e != nil {
		t.Fatal(e)
	}
	if len(workContextTest(t, f, other, anchor).Notes) != 0 {
		t.Fatal("shared task exposed private discussion")
	}
	ownerIn := workInputTest(t, f, f.owner, anchor, "owner-private")
	_ = recordWorkTest(t, f, f.owner, "owner-private", ownerIn)
	if len(workContextTest(t, f, a, anchor).Notes) != 2 {
		t.Fatal("Owner prose flowed into limited MCP")
	}
	after := f.snapshot(t)
	if !reflect.DeepEqual(before.Tasks, after.Tasks) || !reflect.DeepEqual(before.Runs, after.Runs) || !reflect.DeepEqual(before.PlanningRuns, after.PlanningRuns) || !reflect.DeepEqual(before.Decisions, after.Decisions) {
		t.Fatal("discussion changed work/authority")
	}
	if _, e = f.s.RevokeChatCredential(context.Background(), f.owner, "revoke-work", grant.Credential.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = (Coordination{f.s}).Record(context.Background(), a, "next-session", in); e == nil {
		t.Fatal("revoked cached acknowledgement replayed")
	}
	if _, e = (Coordination{f.s}).Context(context.Background(), a, domain.WorkContextQuery{Anchor: anchor, Limit: 1}); e == nil {
		t.Fatal("revoked read")
	}
}

func TestWorkNoteVersionFailureAtomicAndPausedDoesNotExecute(t *testing.T) {
	f := newServiceFixture(t)
	a, _ := chatFixture(t, f)
	anchor := domain.WorkAnchor{Kind: "task", ID: f.task.ID}
	in := workInputTest(t, f, a, anchor, "versioned")
	if e := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
		var task domain.Task
		if e := tx.Get("task", f.task.ID, &task); e != nil {
			return e
		}
		task.Version++
		task.Description = "Revised contract"
		return tx.Put("task", task.ID, task)
	}); e != nil {
		t.Fatal(e)
	}
	before := f.snapshot(t)
	if _, e := (Coordination{f.s}).Record(context.Background(), a, "stale", in); e == nil {
		t.Fatal("stale note accepted")
	}
	if len(workContextTest(t, f, a, anchor).Notes) != 0 || !reflect.DeepEqual(before, f.snapshot(t)) {
		t.Fatal("failed write leaked state/audit")
	}
	in = workInputTest(t, f, a, anchor, "paused")
	if e := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
		p, e := getProject(tx)
		if e != nil {
			return e
		}
		p.Paused = true
		return tx.Put("project", p.ID, p)
	}); e != nil {
		t.Fatal(e)
	}
	_ = recordWorkTest(t, f, a, "paused", in)
	before = f.snapshot(t)
	q := consultationInput(a, 0)
	if _, e := f.s.ConsultLeader(context.Background(), a, "still-paused", "consultation", q); e == nil {
		t.Fatal("coordination bypassed paused Leader policy")
	}
	if !reflect.DeepEqual(before, f.snapshot(t)) {
		t.Fatal("read-only discussion dispatched work")
	}
	forged := in
	forged.Source.ClientID = "owner"
	if _, e := (Coordination{f.s}).Record(context.Background(), a, "forged", forged); e == nil {
		t.Fatal("forged source")
	}
	changed := in
	changed.OriginalText = "different original"
	if _, e := (Coordination{f.s}).Record(context.Background(), a, "new-key", changed); e == nil {
		t.Fatal("same source accepted different body")
	}
}

func TestWorkContextReassignmentRetainsOnlyOwnHistory(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	assignment := f.claim(t, "run-a")
	if assignment == nil {
		t.Fatal("missing run")
	}
	a, _ := nodeChatFixture(t, f, f.node, "node-chat-a")
	nodeB := anotherNode(t, f)
	b, _ := nodeChatFixture(t, f, nodeB, "node-chat-b")
	anchor := domain.WorkAnchor{Kind: "task", ID: f.task.ID}
	in := workInputTest(t, f, a, anchor, "own-run")
	_ = recordWorkTest(t, f, a, "own-run", in)
	if e := f.s.DB.Update(ctx, func(tx *store.Tx) error {
		var task domain.Task
		if e := tx.Get("task", anchor.ID, &task); e != nil {
			return e
		}
		v := assignment.Run
		v.ID = "run_new_node"
		v.NodeID = nodeB.NodeID
		v.Generation++
		task.CurrentRunID = v.ID
		task.Generation = v.Generation
		task.Description = "private replacement contract"
		if e := tx.Put("run", v.ID, v); e != nil {
			return e
		}
		return tx.Put("task", task.ID, task)
	}); e != nil {
		t.Fatal(e)
	}
	forged := a
	forged.ChatScope = domain.ChatScopeCenter
	forged.NodeID = nodeB.NodeID
	v := workContextTest(t, f, forged, anchor)
	encoded, _ := json.Marshal(v)
	if v.Current.Available || !v.Notes[0].Historical || strings.Contains(string(encoded), "replacement") || strings.Contains(string(encoded), "run_new_node") {
		t.Fatal("historical reader saw new execution", string(encoded))
	}
	if _, e := (Coordination{f.s}).Record(ctx, forged, "own-run", in); !errors.Is(e, domain.ErrNotFound) {
		t.Fatal("lost access replayed receipt", e)
	}
	if len(workContextTest(t, f, b, anchor).Notes) != 0 {
		t.Fatal("new assignee inherited private history")
	}
	ref := domain.WorkReference{Kind: "consultation", ID: "consult_guessed"}
	v = workContextTest(t, f, b, anchor, ref)
	if len(v.References) == 0 || v.References[0].Available {
		t.Fatal("node gained Leader access")
	}
	if _, e := f.s.RevokeNode(ctx, f.owner, "revoke-node", f.node.NodeID); e != nil {
		t.Fatal(e)
	}
	if _, e := (Coordination{f.s}).List(ctx, a, domain.WorkListQuery{Limit: 1}); e == nil {
		t.Fatal("revoked node read private history")
	}
}
