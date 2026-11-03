package runtime

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
)

type interruptFixturePort struct {
	workFixturePort
	interrupts atomic.Int32
	interrupt  func(context.Context, WorkInterruptCall) (NativeInterrupt, error)
}

func (p *interruptFixturePort) InterruptWork(ctx context.Context, call WorkInterruptCall) (NativeInterrupt, error) {
	p.interrupts.Add(1)
	if p.interrupt != nil {
		return p.interrupt(ctx, call)
	}
	return NativeInterrupt{Outcome: "acknowledged"}, nil
}

func activeInterruptFixture(t *testing.T, port *interruptFixturePort) (ledgerFixture, *BindingProducer, Binding, EventStream, Work, InterruptWorkRequest) {
	t.Helper()
	f, p, b, s := workFixture(t, port, InterruptTurn)
	w := preparedFixtureWork(t, f, p, b, s)
	w, err := p.ProduceWork(t.Context(), f.cred, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	return f, p, b, s, w, InterruptWorkRequest{Key: "interrupt", WorkID: w.ID, Generation: w.Generation, ExpectedVersion: w.Version}
}

func interruptTerminal(t *testing.T, f ledgerFixture, p *BindingProducer, b Binding, s EventStream, w Work) {
	t.Helper()
	input := eventInput(b, "interrupted", 1)
	input.Kind, input.Fact.State, input.Fact.NativeTurnID = "execution.ended", "interrupted", w.Observed.NativeTurnID
	if _, err := p.AcceptEvent(t.Context(), f.cred, s.ID, input); err != nil {
		t.Fatal(err)
	}
}

func TestInterruptWorkACKDoesNotCompleteReleaseOrReplayWork(t *testing.T) {
	port := &interruptFixturePort{}
	f, p, b, s, w, request := activeInterruptFixture(t, port)
	before, runBefore, slotBefore := readWorkResource(t, f, b)
	command, err := p.InterruptWork(t.Context(), f.cred, request)
	if err != nil || command.State != "acknowledged" || port.interrupts.Load() != 1 {
		t.Fatalf("interrupt: %+v %v", command, err)
	}
	after, runAfter, slotAfter := readWorkResource(t, f, b)
	if before != after || runBefore != runAfter || slotBefore != slotAfter || port.stops.Load() != 0 {
		t.Fatal("ACK changed Work resource or stopped its tree")
	}
	if _, err = p.Release(t.Context(), f.cred, BindingRequest{Key: "release", BindingID: b.ID, Generation: b.Generation, ExpectedVersion: before.Version}); !errors.Is(err, ErrStopUnconfirmed) {
		t.Fatal("interrupt ACK allowed release", err)
	}
	if _, err = p.InterruptWork(t.Context(), f.cred, request); err != nil || port.interrupts.Load() != 1 {
		t.Fatal("replay repeated interrupt", err)
	}
	changed := request
	changed.ExpectedVersion++
	if _, err = p.InterruptWork(t.Context(), f.cred, changed); err == nil {
		t.Fatal("different retry body was accepted")
	}
	changed = request
	changed.Key = "new-key"
	if _, err = p.InterruptWork(t.Context(), f.cred, changed); !errors.Is(err, ErrConflict) || port.interrupts.Load() != 1 {
		t.Fatal("new key repeated native interrupt", err)
	}
	interruptTerminal(t, f, p, b, s, w)
	if _, err = p.InterruptWork(t.Context(), f.cred, request); err != nil || port.interrupts.Load() != 1 {
		t.Fatal("late replay repeated interrupt", err)
	}
	after, _, slotAfter = readWorkResource(t, f, b)
	if after.State != "ready" || after.CleanupStopped || slotAfter.WorkID != "" || port.starts.Load() != 1 || port.stops.Load() != 0 {
		t.Fatal("terminal invented process stop or new Turn")
	}
	reopenFixture(t, &f)
	stored, err := f.ledger.ReadWorkInterrupt(t.Context(), f.cred, w.ID)
	if err != nil || stored.State != "acknowledged" || stored.ID != command.ID || port.interrupts.Load() != 1 {
		t.Fatalf("interrupt reopen: %+v %v", stored, err)
	}
}

func TestInterruptWorkConcurrentClaimAndExpiredAuthorityNeverStop(t *testing.T) {
	t.Run("concurrent", func(t *testing.T) {
		entered, proceed := make(chan struct{}), make(chan struct{})
		port := &interruptFixturePort{interrupt: func(ctx context.Context, _ WorkInterruptCall) (NativeInterrupt, error) {
			close(entered)
			select {
			case <-proceed:
			case <-ctx.Done():
				return NativeInterrupt{Outcome: "unknown"}, ctx.Err()
			}
			return NativeInterrupt{Outcome: "acknowledged"}, nil
		}}
		f, p, _, _, _, request := activeInterruptFixture(t, port)
		done := make(chan error, 1)
		go func() { _, err := p.InterruptWork(t.Context(), f.cred, request); done <- err }()
		<-entered
		replay, err := p.InterruptWork(t.Context(), f.cred, request)
		if err != nil || replay.State != "claimed" || port.interrupts.Load() != 1 {
			t.Fatal("concurrent command repeated", replay, err)
		}
		close(proceed)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	})
	for _, scenario := range []string{"grant-version", "target-config", "owner-generation"} {
		t.Run(scenario, func(t *testing.T) {
			port := &interruptFixturePort{}
			f, p, b, _, w, request := activeInterruptFixture(t, port)
			port.interrupt = func(ctx context.Context, _ WorkInterruptCall) (NativeInterrupt, error) {
				switch scenario {
				case "grant-version":
					f.config.Actions = f.config.Actions[:len(f.config.Actions)-1]
					if _, err := f.manager.ConfigureGrant(ctx, f.config, 2); err != nil {
						t.Fatal(err)
					}
				case "target-config":
					if _, err := f.manager.ConfigureInstance(ctx, Instance{ID: f.target.InstanceID, ApplicationID: f.config.ApplicationID, NodeID: f.target.NodeID, Harness: f.target.Harness, ConfigRef: "changed-config", Support: "unknown", Enabled: true}, 1); err != nil {
						t.Fatal(err)
					}
				case "owner-generation":
					current, _, _ := readWorkResource(t, f, b)
					if _, err := p.RecoverUnknown(ctx, f.cred, BindingRequest{Key: "recover", BindingID: b.ID, Generation: b.Generation, ExpectedVersion: current.Version}); err != nil {
						t.Fatal(err)
					}
				}
				return NativeInterrupt{Outcome: "rejected"}, &privateWorkError{Data: "private-interrupt-error"}
			}
			_, err := p.InterruptWork(t.Context(), f.cred, request)
			var private *privateWorkError
			if !errors.Is(err, ErrOutcomeUnknown) || errors.As(err, &private) || strings.Contains(err.Error(), "private-interrupt-error") || port.stops.Load() != 0 {
				t.Fatal("expired control stopped/exposed native data", err)
			}
			_, _, slot := readWorkResource(t, f, b)
			if slot.WorkID != w.ID || port.interrupts.Load() != 1 || port.starts.Load() != 1 {
				t.Fatal("expired control cleared or replayed Work")
			}
		})
	}
}

func TestInterruptWorkTransactionFailureAndUnsupportedDoNotReplay(t *testing.T) {
	for _, scenario := range []string{"claim-receipt", "ack-persistence", "pre-cancel", "missing-native-id"} {
		t.Run(scenario, func(t *testing.T) {
			port := &interruptFixturePort{}
			if scenario == "missing-native-id" {
				port.start = func(context.Context, WorkCall) (NativeWork, error) {
					return NativeWork{Outcome: "accepted", Evidence: "rpc_accepted", SourceEpoch: "work-source"}, nil
				}
			}
			f, p, b, _, w, request := activeInterruptFixture(t, port)
			before := eventCounts(t, f)
			ctx := t.Context()
			if scenario == "claim-receipt" {
				failReceiptWrites(t, f.path)
			}
			if scenario == "ack-persistence" {
				bindingSQL(t, f.path, `CREATE TRIGGER fixture_interrupt_ack BEFORE UPDATE ON records
 WHEN NEW.kind='runtime.work-interrupt' AND json_extract(NEW.data,'$.state')='acknowledged'
 BEGIN SELECT RAISE(ABORT,'fixture interrupt ack failed'); END`)
			}
			if scenario == "pre-cancel" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			_, err := p.InterruptWork(ctx, f.cred, request)
			if err == nil {
				t.Fatal("failure was accepted")
			}
			if scenario != "ack-persistence" {
				if port.interrupts.Load() != 0 || port.stops.Load() != 0 {
					t.Fatal("unclaimed operation reached native")
				}
				sameEventCounts(t, before, eventCounts(t, f))
				return
			}
			if !errors.Is(err, ErrOutcomeUnknown) || port.interrupts.Load() != 1 || port.stops.Load() != 0 {
				t.Fatal("confirmation failure replayed/stopped", err)
			}
			current, _, slot := readWorkResource(t, f, b)
			if current.State != "unknown" || slot.WorkID != w.ID {
				t.Fatal("confirmation failure released the Work")
			}
			reopenFixture(t, &f)
			stored, err := f.ledger.ReadWorkInterrupt(t.Context(), f.cred, w.ID)
			if err != nil || stored.State != "delivery_unknown" || stored.Observed.Outcome != "acknowledged" || port.interrupts.Load() != 1 {
				t.Fatal("ACK uncertainty did not survive restart", stored, err)
			}
		})
	}
}

func TestInterruptWorkRequiresIndependentAuthorityAndTypedPort(t *testing.T) {
	port := &workFixturePort{}
	f, p, b, s := workFixture(t, port)
	w := preparedFixtureWork(t, f, p, b, s)
	w, err := p.ProduceWork(t.Context(), f.cred, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	request := InterruptWorkRequest{Key: "interrupt", WorkID: w.ID, Generation: w.Generation, ExpectedVersion: w.Version}
	if _, err = p.InterruptWork(t.Context(), f.cred, request); !errors.Is(err, ErrForbidden) {
		t.Fatal("turn.start/session.control granted interrupt", err)
	}
	f.config.Actions = append(f.config.Actions, InterruptTurn)
	if _, err = f.manager.ConfigureGrant(t.Context(), f.config, 2); err != nil {
		t.Fatal(err)
	}
	if _, err = p.InterruptWork(t.Context(), f.cred, request); !errors.Is(err, ErrForbidden) {
		t.Fatal("new permission upgraded old Run", err)
	}
	f, p, b, s = workFixture(t, port, InterruptTurn)
	w = preparedFixtureWork(t, f, p, b, s)
	w, err = p.ProduceWork(t.Context(), f.cred, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	request.WorkID, request.Generation, request.ExpectedVersion = w.ID, w.Generation, w.Version
	if _, err = p.InterruptWork(t.Context(), f.cred, request); !errors.Is(err, ErrUnsupported) || port.stops.Load() != 0 {
		t.Fatal("unsupported port fell back to Stop", err)
	}
}
