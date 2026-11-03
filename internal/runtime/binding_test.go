package runtime

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

type countedBindingPort struct {
	creates atomic.Int32
	stops   atomic.Int32
	create  func(context.Context, NativeCreate) (NativeCreation, error)
	stop    func(context.Context, NativeCreate, NativeSessionID) (BindingStop, error)
}

func (p *countedBindingPort) Create(ctx context.Context, call NativeCreate) (NativeCreation, error) {
	p.creates.Add(1)
	if p.create != nil {
		return p.create(ctx, call)
	}
	return NativeCreation{Outcome: "confirmed", ID: NativeSessionID{Harness: call.Config.Target.Harness, Value: "native-" + call.BindingID}, Effective: call.Config}, nil
}

func (p *countedBindingPort) Stop(ctx context.Context, call NativeCreate, id NativeSessionID) (BindingStop, error) {
	p.stops.Add(1)
	if p.stop != nil {
		return p.stop(ctx, call, id)
	}
	return BindingStop{BindingID: call.BindingID, OwnerID: call.OwnerID, NativeID: id, TreeStopped: true}, nil
}

func bindingFixture(t *testing.T, port BindingPort) (ledgerFixture, *BindingProducer, TaskSnapshot, RunSnapshot) {
	t.Helper()
	f := fixtureLedger(t)
	f.config.Actions = append(f.config.Actions, SessionControl)
	if _, err := f.manager.ConfigureGrant(t.Context(), f.config, 1); err != nil {
		t.Fatal(err)
	}
	task := submitFixture(t, f, "task")
	run := registerFixture(t, f, task, "run")
	config := BindingConfig{Target: f.target, InstanceConfigRef: "fixture-config-v1", ProfileRef: "fixture-profile",
		WorkspaceFingerprint: "fixture-workspace", ProtocolVersion: "fixture-v1", ApprovalPolicy: "on-request", Sandbox: "read-only"}
	p, err := NewBindingProducer(f.db, config, port)
	if err != nil {
		t.Fatal(err)
	}
	return f, p, task, run
}

func prepareFixture(t *testing.T, f ledgerFixture, p *BindingProducer, task TaskSnapshot, run RunSnapshot) Binding {
	t.Helper()
	binding, err := p.PrepareBinding(t.Context(), f.cred, PrepareBindingRequest{Key: "prepare", TaskID: task.Task.ID,
		RunID: run.Run.ID, RunGeneration: run.Run.Generation, ExpectedVersion: 2})
	if err != nil {
		t.Fatal(err)
	}
	return binding
}

func TestBindingPrepareProduceSQLiteAndDuplicateDoesNotCreateAgain(t *testing.T) {
	port := &countedBindingPort{}
	f, p, task, run := bindingFixture(t, port)
	binding := prepareFixture(t, f, p, task, run)
	if port.creates.Load() != 0 || binding.State != "creating" {
		t.Fatal("prepare performed native work", binding)
	}
	ready, err := p.Produce(t.Context(), f.cred, binding.ID)
	if err != nil || ready.State != "ready" || ready.NativeID.Value == "" || port.creates.Load() != 1 {
		t.Fatalf("binding failed: %+v %v", ready, err)
	}
	if _, err := p.Produce(t.Context(), f.cred, binding.ID); !errors.Is(err, ErrOutcomeUnknown) || port.creates.Load() != 1 {
		t.Fatalf("duplicate producer repeated native create: %v count=%d", err, port.creates.Load())
	}
	reopenFixture(t, &f)
	got, err := f.ledger.ReadBinding(t.Context(), f.cred, binding.ID)
	if err != nil || got != ready || port.creates.Load() != 1 {
		t.Fatalf("reopen lost binding or performed work: %+v %v", got, err)
	}
}

func TestBindingControlIsNotInheritedFromRunRegistration(t *testing.T) {
	f := fixtureLedger(t)
	task := submitFixture(t, f, "task")
	run := registerFixture(t, f, task, "run")
	port := &countedBindingPort{}
	config := BindingConfig{Target: f.target, InstanceConfigRef: "fixture-config-v1", ProfileRef: "fixture-profile",
		WorkspaceFingerprint: "fixture-workspace", ProtocolVersion: "fixture-v1", ApprovalPolicy: "on-request", Sandbox: "read-only"}
	p, err := NewBindingProducer(f.db, config, port)
	if err != nil {
		t.Fatal(err)
	}
	request := PrepareBindingRequest{Key: "prepare", TaskID: task.Task.ID, RunID: run.Run.ID, RunGeneration: 1, ExpectedVersion: 2}
	if _, err := p.PrepareBinding(t.Context(), f.cred, request); !errors.Is(err, ErrForbidden) {
		t.Fatalf("old ledger grant gained native authority: %v", err)
	}
	f.config.Actions = append(f.config.Actions, SessionControl)
	if _, err := f.manager.ConfigureGrant(t.Context(), f.config, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := p.PrepareBinding(t.Context(), f.cred, request); !errors.Is(err, ErrForbidden) || port.creates.Load() != 0 {
		t.Fatalf("new grant silently replaced old Run authorization: %v", err)
	}
}
