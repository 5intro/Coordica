package runtime

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

type workFixturePort struct {
	countedBindingPort
	starts atomic.Int32
	start  func(context.Context, WorkCall) (NativeWork, error)
}

func (p *workFixturePort) StartWork(ctx context.Context, call WorkCall) (NativeWork, error) {
	p.starts.Add(1)
	if p.start != nil {
		return p.start(ctx, call)
	}
	return NativeWork{Outcome: "accepted", NativeTurnID: "native-work", State: "running", Evidence: "rpc_accepted", SourceEpoch: "work-source"}, nil
}
func workFixture(t *testing.T, port BindingPort) (ledgerFixture, *BindingProducer, Binding, EventStream) {
	t.Helper()
	f := fixtureLedger(t)
	f.config.Actions = append(f.config.Actions, SessionControl, StartWork)
	if _, err := f.manager.ConfigureGrant(t.Context(), f.config, 1); err != nil {
		t.Fatal(err)
	}
	task := submitFixture(t, f, "task")
	run := registerFixture(t, f, task, "run")
	p, err := NewBindingProducer(f.db, BindingConfig{Target: f.target, InstanceConfigRef: "fixture-config-v1", ProfileRef: "fixture-profile", WorkspaceFingerprint: "fixture-workspace", ProtocolVersion: "fixture", ApprovalPolicy: "on-request", Sandbox: "read-only"}, port)
	if err != nil {
		t.Fatal(err)
	}
	b := prepareFixture(t, f, p, task, run)
	b, err = p.Produce(t.Context(), f.cred, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	s, err := p.OpenEventStream(t.Context(), f.cred, b.ID, SourceStamp{Epoch: "work-source", Evidence: "native_sequence"})
	if err != nil {
		t.Fatal(err)
	}
	return f, p, b, s
}
func TestWorkPrepareSingleClaimAndReopenNeverReplaysInput(t *testing.T) {
	port := &workFixturePort{}
	f, p, b, s := workFixture(t, port)
	request := PrepareWorkRequest{Key: "work", BindingID: b.ID, StreamID: s.ID, Generation: b.Generation, ExpectedVersion: b.Version, Text: "explicit private work"}
	w, err := p.PrepareWork(t.Context(), f.cred, request)
	if err != nil || port.starts.Load() != 0 {
		t.Fatalf("prepare started native work: %+v %v", w, err)
	}
	if _, err = p.PrepareWork(t.Context(), f.cred, request); err != nil {
		t.Fatal(err)
	}
	request.Key = "different"
	if _, err = p.PrepareWork(t.Context(), f.cred, request); !errors.Is(err, ErrWorkBusy) {
		t.Fatal(err)
	}
	w, err = p.ProduceWork(t.Context(), f.cred, w.ID)
	if err != nil || w.State != "native_accepted" || port.starts.Load() != 1 {
		t.Fatalf("native start: %+v %v", w, err)
	}
	if _, err = p.ProduceWork(t.Context(), f.cred, w.ID); !errors.Is(err, ErrOutcomeUnknown) || port.starts.Load() != 1 {
		t.Fatal("duplicate repeated work", err)
	}
	if _, err = p.Release(t.Context(), f.cred, BindingRequest{Key: "release-busy", BindingID: b.ID, ExpectedVersion: b.Version + 2, Generation: b.Generation}); !errors.Is(err, ErrStopUnconfirmed) || port.stops.Load() != 0 {
		t.Fatal("busy release stopped work", err)
	}
	reopenFixture(t, &f)
	after, err := f.ledger.ReadWork(t.Context(), f.cred, w.ID)
	if err != nil || after.ID != w.ID || after.State != "native_accepted" || port.starts.Load() != 1 {
		t.Fatalf("work reopen: %+v %v", after, err)
	}
}
