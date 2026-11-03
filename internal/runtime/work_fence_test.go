package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"

	"coordica/internal/store"
)

type privateWorkError struct{ Data string }

func (e *privateWorkError) Error() string { return e.Data }
func preparedFixtureWork(t *testing.T, f ledgerFixture, p *BindingProducer, b Binding, s EventStream) Work {
	t.Helper()
	w, err := p.PrepareWork(t.Context(), f.cred, PrepareWorkRequest{Key: "work", BindingID: b.ID, StreamID: s.ID, Generation: b.Generation, ExpectedVersion: b.Version, Text: "explicit-private-work"})
	if err != nil {
		t.Fatal(err)
	}
	return w
}
func readWorkResource(t *testing.T, f ledgerFixture, b Binding) (Binding, Run, workSlot) {
	t.Helper()
	var current Binding
	var run Run
	var slot workSlot
	if err := f.db.View(t.Context(), func(tx *store.Tx) error {
		if err := tx.Get(kindBinding, b.ID, &current); err != nil {
			return err
		}
		if err := tx.Get(kindRun, b.RunID, &run); err != nil {
			return err
		}
		return tx.Get(kindWorkSlot, b.ID, &slot)
	}); err != nil {
		t.Fatal(err)
	}
	return current, run, slot
}
func TestWorkExpiredAuthorityDoesNotAuthorizeCleanupOrExposePrivateErrors(t *testing.T) {
	for _, scenario := range []string{"revoke-execute", "target-config", "owner-generation"} {
		t.Run(scenario, func(t *testing.T) {
			port := &workFixturePort{}
			f, p, b, s := workFixture(t, port)
			w := preparedFixtureWork(t, f, p, b, s)
			port.start = func(ctx context.Context, call WorkCall) (NativeWork, error) {
				switch scenario {
				case "revoke-execute":
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
					if _, err := p.RecoverUnknown(ctx, f.cred, BindingRequest{Key: "recover", BindingID: b.ID, ExpectedVersion: current.Version, Generation: current.Generation}); err != nil {
						t.Fatal(err)
					}
				}
				return NativeWork{Outcome: "unknown", NativeTurnID: "observed-work", SourceEpoch: s.Source.Epoch}, &privateWorkError{Data: "private-native-work-error"}
			}
			_, err := p.ProduceWork(t.Context(), f.cred, w.ID)
			var private *privateWorkError
			if !errors.Is(err, ErrOutcomeUnknown) || errors.As(err, &private) || strings.Contains(err.Error(), "private-native-work") || port.stops.Load() != 0 {
				t.Fatalf("expired command cleaned/exposed native data: %v stops=%d", err, port.stops.Load())
			}
			_, _, slot := readWorkResource(t, f, b)
			if slot.WorkID != w.ID || port.starts.Load() != 1 {
				t.Fatal("unknown work slot was cleared/replayed")
			}
		})
	}
}

func TestLateStartReplyAndTerminalCannotReviveUnknownSource(t *testing.T) {
	for _, stopped := range []bool{false, true} {
		t.Run(map[bool]string{false: "unconfirmed-stop", true: "known-stop"}[stopped], func(t *testing.T) {
			port := &workFixturePort{}
			f, p, b, s := workFixture(t, port)
			w := preparedFixtureWork(t, f, p, b, s)
			port.start = func(ctx context.Context, call WorkCall) (NativeWork, error) {
				input := eventInput(b, "early-terminal", 1)
				input.Kind = "execution.ended"
				input.Fact.State = "completed"
				input.Fact.NativeTurnID = "native-work"
				if _, err := p.AcceptEvent(ctx, f.cred, s.ID, input); err != nil {
					t.Fatal(err)
				}
				var proof BindingStop
				if stopped {
					proof = BindingStop{BindingID: b.ID, OwnerID: b.OwnerID, NativeID: b.NativeID, TreeStopped: true}
				}
				if err := p.ReportSourceEnd(ctx, f.cred, s.ID, SourceEnd{Reason: "transport_lost", Gap: true, LastObserved: 1, Stop: proof}); err != nil {
					t.Fatal(err)
				}
				return NativeWork{Outcome: "accepted", NativeTurnID: "native-work", State: "running", Evidence: "rpc_accepted", SourceEpoch: s.Source.Epoch}, nil
			}
			_, _ = p.ProduceWork(t.Context(), f.cred, w.ID)
			current, run, slot := readWorkResource(t, f, b)
			if current.State != "unknown" || run.NativeDisposition != "session_unknown" || slot.WorkID != w.ID || port.starts.Load() != 1 {
				t.Fatalf("late ACK revived unknown resource: Binding=%+v Run=%+v Slot=%+v", current, run, slot)
			}
		})
	}
}

type failedWorkReadPort struct{ workFixturePort }

func (p *failedWorkReadPort) Resume(context.Context, NativeCreate, NativeSessionID) (NativeCreation, error) {
	return NativeCreation{}, ErrInvalid
}
func (p *failedWorkReadPort) Read(context.Context, NativeCreate, NativeSessionID, bool) (NativeSnapshot, error) {
	return NativeSnapshot{Outcome: "unknown"}, errors.New("fixture read disconnected")
}

func TestLateWorkTerminalAfterReadQuarantineDoesNotReviveBinding(t *testing.T) {
	port := &failedWorkReadPort{}
	f, p, b, s := workFixture(t, port)
	w := preparedFixtureWork(t, f, p, b, s)
	if _, err := p.ProduceWork(t.Context(), f.cred, w.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := p.ReadNative(t.Context(), f.cred, b.ID, true); !errors.Is(err, ErrOutcomeUnknown) {
		t.Fatal(err)
	}
	before, runBefore, _ := readWorkResource(t, f, b)
	input := eventInput(b, "late-terminal", 1)
	input.Kind = "execution.ended"
	input.Fact.State = "completed"
	input.Fact.NativeTurnID = "native-work"
	if _, err := p.AcceptEvent(t.Context(), f.cred, s.ID, input); err != nil {
		t.Fatal(err)
	}
	after, runAfter, slot := readWorkResource(t, f, b)
	if before != after || runBefore != runAfter || after.State != "unknown" || slot.WorkID != w.ID {
		t.Fatalf("late terminal revived quarantined lifecycle: Binding=%+v Slot=%+v", after, slot)
	}
}

type scopedWorkReadPort struct {
	workFixturePort
	read func(context.Context) (NativeSnapshot, error)
}

func (p *scopedWorkReadPort) Resume(context.Context, NativeCreate, NativeSessionID) (NativeCreation, error) {
	return NativeCreation{}, ErrInvalid
}
func (p *scopedWorkReadPort) Read(ctx context.Context, _ NativeCreate, _ NativeSessionID, _ bool) (NativeSnapshot, error) {
	return p.read(ctx)
}

func TestBusyReadExpiredAuthorityCannotStopActiveWork(t *testing.T) {
	for _, scenario := range []string{"revoke-execute", "grant-version", "target-config"} {
		t.Run(scenario, func(t *testing.T) {
			port := &scopedWorkReadPort{}
			f, p, b, s := workFixture(t, port)
			w := preparedFixtureWork(t, f, p, b, s)
			if _, err := p.ProduceWork(t.Context(), f.cred, w.ID); err != nil {
				t.Fatal(err)
			}
			before, runBefore, slotBefore := readWorkResource(t, f, b)
			port.read = func(ctx context.Context) (NativeSnapshot, error) {
				if scenario == "target-config" {
					if _, err := f.manager.ConfigureInstance(ctx, Instance{ID: f.target.InstanceID, ApplicationID: f.config.ApplicationID, NodeID: f.target.NodeID, Harness: f.target.Harness, ConfigRef: "changed-config", Support: "unknown", Enabled: true}, 1); err != nil {
						t.Fatal(err)
					}
				} else {
					if scenario == "revoke-execute" {
						f.config.Actions = f.config.Actions[:len(f.config.Actions)-1]
					}
					if _, err := f.manager.ConfigureGrant(ctx, f.config, 2); err != nil {
						t.Fatal(err)
					}
				}
				if scenario == "revoke-execute" {
					return NativeSnapshot{Outcome: "confirmed", History: []byte(`{"private":"expired-work-read"}`)}, nil
				}
				return NativeSnapshot{Outcome: "unknown"}, &privateWorkError{Data: "expired-work-read"}
			}
			raw, err := p.ReadNative(t.Context(), f.cred, b.ID, true)
			var private *privateWorkError
			if !errors.Is(err, ErrOutcomeUnknown) || len(raw) != 0 || errors.As(err, &private) || strings.Contains(err.Error(), "expired-work-read") || port.stops.Load() != 0 {
				t.Fatalf("expired busy read cleaned/exposed work: %s %v stops=%d", raw, err, port.stops.Load())
			}
			after, runAfter, slotAfter := readWorkResource(t, f, b)
			if before != after || runBefore != runAfter || slotBefore != slotAfter || slotAfter.WorkID != w.ID {
				t.Fatal("expired reader changed the active resource or Work slot")
			}
		})
	}
}
