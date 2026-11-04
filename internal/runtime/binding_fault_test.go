package runtime

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"coordica/internal/store"
)

func bindingSQL(t *testing.T, path, query string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(query); err != nil {
		t.Fatal(err)
	}
}

func readBindingHost(t *testing.T, f ledgerFixture, id string) (Binding, BindingCommand) {
	t.Helper()
	var b Binding
	var c BindingCommand
	err := f.db.View(t.Context(), func(tx *store.Tx) error {
		if err := tx.Get(kindBinding, id, &b); err != nil {
			return err
		}
		return tx.Get(kindBindingCommand, b.CommandID, &c)
	})
	if err != nil {
		t.Fatal(err)
	}
	return b, c
}

func TestBindingPrepareRollbackAndConfirmationFailureNeverRepeatCreate(t *testing.T) {
	t.Run("prepare-rollback", func(t *testing.T) {
		port := &countedBindingPort{}
		f, p, task, run := bindingFixture(t, port)
		before := storageCounts(t, f.path)
		failReceiptWrites(t, f.path)
		_, err := p.PrepareBinding(t.Context(), f.cred, PrepareBindingRequest{Key: "prepare", TaskID: task.Task.ID, RunID: run.Run.ID, RunGeneration: 1, ExpectedVersion: 2})
		if err == nil || port.creates.Load() != 0 {
			t.Fatalf("rolled-back prepare called native: %v", err)
		}
		after := storageCounts(t, f.path)
		for table, count := range before {
			if after[table] != count {
				t.Fatalf("partial prepare survived: %v -> %v", before, after)
			}
		}
		if got := readFixture(t, f, task.Task.ID); got.Task.CurrentRunID != run.Run.ID || got.Task.Version != 2 {
			t.Fatal(got)
		}
	})
	t.Run("created-before-confirmation-failure", func(t *testing.T) {
		port := &countedBindingPort{}
		f, p, task, run := bindingFixture(t, port)
		b := prepareFixture(t, f, p, task, run)
		bindingSQL(t, f.path, `CREATE TRIGGER fixture_fail_binding_ready BEFORE UPDATE ON records
 WHEN NEW.kind='runtime.binding' AND json_extract(NEW.data,'$.state')='ready'
 BEGIN SELECT RAISE(ABORT,'fixture confirmation failure'); END`)
		if _, err := p.Produce(t.Context(), f.cred, b.ID); !errors.Is(err, ErrOutcomeUnknown) {
			t.Fatalf("lost confirmation became safe retry: %v", err)
		}
		got, command := readBindingHost(t, f, b.ID)
		if got.State != "unknown" || got.NativeID.Value == "" || command.Observed.ID != got.NativeID || command.State != "unknown" {
			t.Fatalf("native observation lost: %+v %+v", got, command)
		}
		if _, err := p.Produce(t.Context(), f.cred, b.ID); !errors.Is(err, ErrOutcomeUnknown) || port.creates.Load() != 1 {
			t.Fatalf("unknown retried create: %v count=%d", err, port.creates.Load())
		}
		if _, err := f.ledger.CancelRun(t.Context(), f.cred, CancelRequest{Key: "cancel", TaskID: task.Task.ID, ExpectedVersion: 3, RunID: run.Run.ID, RunGeneration: 1}); !errors.Is(err, ErrStopUnconfirmed) {
			t.Fatalf("unknown cancelled as stopped: %v", err)
		}
	})
}

func TestBindingConcurrentClaimRecoveryAndLateOwnerAreFenced(t *testing.T) {
	entered, finish := make(chan struct{}, 1), make(chan struct{})
	port := &countedBindingPort{create: func(ctx context.Context, call NativeCreate) (NativeCreation, error) {
		entered <- struct{}{}
		select {
		case <-finish:
		case <-ctx.Done():
			return NativeCreation{Outcome: "unknown"}, ctx.Err()
		}
		return NativeCreation{Outcome: "confirmed", ID: NativeSessionID{Harness: call.Config.Target.Harness, Value: "late-native"}, Effective: call.Config}, nil
	}}
	f, p, task, run := bindingFixture(t, port)
	b := prepareFixture(t, f, p, task, run)
	done := make(chan error, 1)
	go func() { _, err := p.Produce(t.Context(), f.cred, b.ID); done <- err }()
	<-entered
	other, err := NewBindingProducer(f.db, p.config, port)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Produce(t.Context(), f.cred, b.ID); !errors.Is(err, ErrOutcomeUnknown) || port.creates.Load() != 1 {
		t.Fatalf("second owner duplicated create: %v", err)
	}
	if _, err := f.ledger.CancelTask(t.Context(), f.cred, CancelRequest{Key: "cancel-pending", TaskID: task.Task.ID, ExpectedVersion: 3}); !errors.Is(err, ErrStopUnconfirmed) {
		t.Fatalf("inflight resource cancelled as stopped: %v", err)
	}
	current, _ := readBindingHost(t, f, b.ID)
	recovered, err := other.RecoverUnknown(t.Context(), f.cred, BindingRequest{Key: "recover", BindingID: b.ID, ExpectedVersion: current.Version, Generation: current.Generation})
	if err != nil || recovered.Generation != 2 || recovered.State != "unknown" || recovered.OwnerID != "" {
		t.Fatalf("recovery was a native takeover: %+v %v", recovered, err)
	}
	close(finish)
	if err := <-done; !errors.Is(err, ErrOutcomeUnknown) {
		t.Fatalf("late owner installed binding: %v", err)
	}
	after, command := readBindingHost(t, f, b.ID)
	if after != recovered || command.Observed.ID.Value != "late-native" || command.Generation != 1 {
		t.Fatalf("late result advanced new generation or lost original observation: %+v %+v", after, command)
	}
	reopenFixture(t, &f)
	newOwner, err := NewBindingProducer(f.db, p.config, port)
	if err != nil {
		t.Fatal(err)
	}
	if port.creates.Load() != 1 {
		t.Fatal("constructor performed recovery I/O")
	}
	if _, err := newOwner.Produce(t.Context(), f.cred, b.ID); !errors.Is(err, ErrOutcomeUnknown) || port.creates.Load() != 1 {
		t.Fatalf("restart replayed native creation: %v", err)
	}
}

func TestBindingCurrentAuthorityAndEffectiveConfigurationRemainFenced(t *testing.T) {
	for _, scenario := range []string{"revoked-during-create", "configuration-mismatch"} {
		t.Run(scenario, func(t *testing.T) {
			port := &countedBindingPort{}
			f, p, task, run := bindingFixture(t, port)
			b := prepareFixture(t, f, p, task, run)
			port.create = func(ctx context.Context, call NativeCreate) (NativeCreation, error) {
				config := call.Config
				if scenario == "revoked-during-create" {
					f.config.Enabled = false
					if _, err := f.manager.ConfigureGrant(ctx, f.config, 2); err != nil {
						return NativeCreation{Outcome: "unknown"}, err
					}
				} else {
					config.Target.Model = "wrong-model"
				}
				return NativeCreation{Outcome: "confirmed", ID: NativeSessionID{Harness: call.Config.Target.Harness, Value: "observed-native"}, Effective: config}, nil
			}
			if _, err := p.Produce(t.Context(), f.cred, b.ID); !errors.Is(err, ErrOutcomeUnknown) {
				t.Fatalf("invalid completion published ready: %v", err)
			}
			got, command := readBindingHost(t, f, b.ID)
			if got.State != "unknown" || got.Outcome != "unknown" || command.Observed.Outcome != "confirmed" ||
				command.Observed.ID.Value != "observed-native" || port.stops.Load() != 1 {
				t.Fatalf("observed resource was discarded or not quarantined: %+v %+v", got, command)
			}
			if scenario == "revoked-during-create" {
				if _, err := f.ledger.ReadBinding(t.Context(), f.cred, b.ID); !errors.Is(err, ErrForbidden) {
					t.Fatalf("revoked caller read observation: %v", err)
				}
			}
		})
	}
}

func TestBindingStopProofAndExplicitFailureClassification(t *testing.T) {
	t.Run("missing-stop-proof", func(t *testing.T) {
		port := &countedBindingPort{stop: func(context.Context, NativeCreate, NativeSessionID) (BindingStop, error) { return BindingStop{}, nil }}
		f, p, task, run := bindingFixture(t, port)
		b := prepareFixture(t, f, p, task, run)
		ready, err := p.Produce(t.Context(), f.cred, b.ID)
		if err != nil {
			t.Fatal(err)
		}
		_, err = p.Release(t.Context(), f.cred, BindingRequest{Key: "release", BindingID: b.ID, ExpectedVersion: ready.Version, Generation: ready.Generation})
		if !errors.Is(err, ErrStopUnconfirmed) || !errors.Is(err, ErrOutcomeUnknown) {
			t.Fatalf("nil stop error invented stop proof: %v", err)
		}
		got, _ := readBindingHost(t, f, b.ID)
		if got.State != "unknown" || got.CleanupStopped {
			t.Fatal(got)
		}
	})
	t.Run("definite-rejection", func(t *testing.T) {
		rejected := errors.New("fixture native rejection")
		port := &countedBindingPort{create: func(context.Context, NativeCreate) (NativeCreation, error) {
			return NativeCreation{Outcome: "rejected"}, rejected
		}}
		f, p, task, run := bindingFixture(t, port)
		b := prepareFixture(t, f, p, task, run)
		got, err := p.Produce(t.Context(), f.cred, b.ID)
		if !errors.Is(err, rejected) || errors.Is(err, ErrOutcomeUnknown) || got.State != "unbound" {
			t.Fatalf("explicit rejection was misclassified: %+v %v", got, err)
		}
		reopenFixture(t, &f)
		persisted, command := readBindingHost(t, f, b.ID)
		storedRun, err := f.ledger.ReadRun(t.Context(), f.cred, task.Task.ID, run.Run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !persisted.CleanupStopped || !command.Stop.TreeStopped || command.Stop.BindingID != b.ID ||
			command.Stop.OwnerID != persisted.OwnerID || command.Stop.NativeID != (NativeSessionID{}) || storedRun.Run.NativeDisposition != "not_started" {
			t.Fatalf("safe failure lost durable stop evidence: %+v %+v %+v", persisted, command, storedRun)
		}
	})
	t.Run("cancelled-before-call", func(t *testing.T) {
		port := &countedBindingPort{}
		f, p, task, run := bindingFixture(t, port)
		b := prepareFixture(t, f, p, task, run)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if _, err := p.Produce(ctx, f.cred, b.ID); !errors.Is(err, context.Canceled) || errors.Is(err, ErrOutcomeUnknown) || port.creates.Load() != 0 {
			t.Fatalf("unsent call was dispatched or uncertain: %v", err)
		}
	})
}
