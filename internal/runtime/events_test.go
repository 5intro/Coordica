package runtime

import (
	"errors"
	"testing"
	"time"
)

func eventFixture(t *testing.T) (ledgerFixture, *BindingProducer, Binding, EventStream) {
	t.Helper()
	f, p, task, run := bindingFixture(t, &countedBindingPort{})
	b := prepareFixture(t, f, p, task, run)
	b, err := p.Produce(t.Context(), f.cred, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	s, err := p.OpenEventStream(t.Context(), f.cred, b.ID, SourceStamp{Epoch: "fixture-peer-epoch", Evidence: "native_sequence"})
	if err != nil {
		t.Fatal(err)
	}
	return f, p, b, s
}
func eventInput(b Binding, id string, sequence int64) EventInput {
	return EventInput{EventID: id, SourceSequence: sequence, NativeID: b.NativeID, Kind: "output.delta", OccurredAt: time.Unix(100, 0).UTC(), Fact: EventFact{Bytes: 5}}
}

func TestEventSQLiteAcceptPageAckAndRestartKeepDatasetEpoch(t *testing.T) {
	f, p, b, stream := eventFixture(t)
	input := eventInput(b, "event-1", 1)
	first, err := p.AcceptEvent(t.Context(), f.cred, stream.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := p.AcceptEvent(t.Context(), f.cred, stream.ID, input)
	if err != nil || !duplicate.Duplicate || duplicate.Sequence != first.Sequence {
		t.Fatalf("duplicate: %+v %v", duplicate, err)
	}
	if _, err := p.AcceptEvent(t.Context(), f.cred, stream.ID, eventInput(b, "event-2", 2)); err != nil {
		t.Fatal(err)
	}
	cursor, err := f.ledger.OpenEventCursor(t.Context(), f.cred, stream.ID)
	if err != nil {
		t.Fatal(err)
	}
	page, err := f.ledger.ReadEvents(t.Context(), f.cred, cursor.ID, 1)
	if err != nil || len(page.Events) != 1 || !page.More || page.Events[0].Sequence != first.Sequence {
		t.Fatalf("page: %+v %v", page, err)
	}
	if _, err := f.ledger.AckEvents(t.Context(), f.cred, cursor.ID, page.Cursor.Offered+100); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if _, err := f.ledger.AckEvents(t.Context(), f.cred, cursor.ID, first.Sequence); err != nil {
		t.Fatal(err)
	}
	reopenFixture(t, &f)
	page, err = f.ledger.ReadEvents(t.Context(), f.cred, cursor.ID, 64)
	if err != nil || len(page.Events) != 1 || page.Events[0].Input.EventID != "event-2" || page.Stream.DatasetEpoch != stream.DatasetEpoch {
		t.Fatalf("reopen cursor: %+v %v", page, err)
	}
}

func TestEventLedgerUsesSameContractForAnotherRegisteredHarness(t *testing.T) {
	f := fixtureLedger(t)
	f.config.Actions = append(f.config.Actions, SessionControl)
	f.config.Targets = append(f.config.Targets, TargetScope{InstanceID: "instance-acp", NodeID: "node-acp"})
	if _, err := f.manager.ConfigureGrant(t.Context(), f.config, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := f.manager.ConfigureInstance(t.Context(), Instance{ID: "instance-acp", ApplicationID: f.config.ApplicationID, NodeID: "node-acp", Harness: "acp", ConfigRef: "fixture-config-v1", Support: "unknown", Enabled: true}, 0); err != nil {
		t.Fatal(err)
	}
	f.target.InstanceID, f.target.NodeID, f.target.Harness = "instance-acp", "node-acp", "acp"
	config := BindingConfig{Target: f.target, InstanceConfigRef: "fixture-config-v1", ProfileRef: "fixture-profile", WorkspaceFingerprint: "fixture-workspace", ProtocolVersion: "fixture-acp", ApprovalPolicy: "on-request", Sandbox: "read-only"}
	p, err := NewBindingProducer(f.db, config, &countedBindingPort{})
	if err != nil {
		t.Fatal(err)
	}
	task := submitFixture(t, f, "acp-task")
	run := registerFixture(t, f, task, "acp-run")
	binding := prepareFixture(t, f, p, task, run)
	binding, err = p.Produce(t.Context(), f.cred, binding.ID)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := p.OpenEventStream(t.Context(), f.cred, binding.ID, SourceStamp{Epoch: "fixture-acp-source", Evidence: "native_sequence"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.AcceptEvent(t.Context(), f.cred, stream.ID, eventInput(binding, "acp-event", 1)); err != nil {
		t.Fatal(err)
	}
	c, err := f.ledger.OpenEventCursor(t.Context(), f.cred, stream.ID)
	if err != nil {
		t.Fatal(err)
	}
	page, err := f.ledger.ReadEvents(t.Context(), f.cred, c.ID, 1)
	if err != nil || len(page.Events) != 1 || page.Stream.NativeID.Harness != "acp" {
		t.Fatalf("generic event flow depended on Codex: %+v %v", page, err)
	}
}
