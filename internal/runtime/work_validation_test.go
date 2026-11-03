package runtime

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
)

func TestWorkConcurrentProduceClaimsOneNativeStart(t *testing.T) {
	entered, release := make(chan struct{}, 1), make(chan struct{})
	port := &workFixturePort{start: func(ctx context.Context, call WorkCall) (NativeWork, error) {
		entered <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
			return NativeWork{Outcome: "unknown", SourceEpoch: "work-source"}, ctx.Err()
		}
		return NativeWork{Outcome: "accepted", NativeTurnID: "native-work", State: "running", Evidence: "rpc_accepted", SourceEpoch: "work-source"}, nil
	}}
	f, p, b, s := workFixture(t, port)
	w := preparedFixtureWork(t, f, p, b, s)
	done := make(chan error, 1)
	go func() { _, err := p.ProduceWork(t.Context(), f.cred, w.ID); done <- err }()
	<-entered
	if _, err := p.ProduceWork(t.Context(), f.cred, w.ID); !errors.Is(err, ErrOutcomeUnknown) || port.starts.Load() != 1 {
		t.Fatal("concurrent claim repeated native work", err)
	}
	if _, err := f.ledger.CancelTask(t.Context(), f.cred, CancelRequest{Key: "cancel-work", TaskID: b.TaskID, ExpectedVersion: 3}); !errors.Is(err, ErrStopUnconfirmed) {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestWorkTransactionFailuresDoNotRepeatOrLoseUnknownEvidence(t *testing.T) {
	for _, scenario := range []string{"prepare-receipt", "native-confirmation"} {
		t.Run(scenario, func(t *testing.T) {
			port := &workFixturePort{}
			f, p, b, s := workFixture(t, port)
			if scenario == "prepare-receipt" {
				before := eventCounts(t, f)
				failReceiptWrites(t, f.path)
				_, err := p.PrepareWork(t.Context(), f.cred, PrepareWorkRequest{Key: "work", BindingID: b.ID, StreamID: s.ID, Generation: b.Generation, ExpectedVersion: b.Version, Text: "explicit-work"})
				if err == nil || port.starts.Load() != 0 {
					t.Fatal("rolled-back intent started work")
				}
				sameEventCounts(t, before, eventCounts(t, f))
				return
			}
			w := preparedFixtureWork(t, f, p, b, s)
			bindingSQL(t, f.path, `CREATE TRIGGER fixture_work_confirmation BEFORE UPDATE ON records
 WHEN NEW.kind='runtime.work' AND json_extract(NEW.data,'$.state')='native_accepted'
 BEGIN SELECT RAISE(ABORT,'fixture work confirmation failed'); END`)
			if _, err := p.ProduceWork(t.Context(), f.cred, w.ID); !errors.Is(err, ErrOutcomeUnknown) || port.starts.Load() != 1 || port.stops.Load() != 1 {
				t.Fatalf("confirmation failure became retryable: %v", err)
			}
			reopenFixture(t, &f)
			after, err := f.ledger.ReadWork(t.Context(), f.cred, w.ID)
			if err != nil || after.State != "delivery_unknown" || after.Observed.NativeTurnID != "native-work" || !after.Stop.TreeStopped {
				t.Fatalf("durable unknown/stop observation lost: %+v %v", after, err)
			}
			newProducer, err := NewBindingProducer(f.db, b.Config, port)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = newProducer.ProduceWork(t.Context(), f.cred, w.ID); err == nil || port.starts.Load() != 1 {
				t.Fatal("restart replayed unknown work")
			}
		})
	}
}

func TestWorkAllowsNativeTerminalEvidenceWithoutInventingNativeTurnID(t *testing.T) {
	port := &workFixturePort{start: func(context.Context, WorkCall) (NativeWork, error) {
		return NativeWork{Outcome: "ended", State: "completed", Evidence: "rpc_terminal", SourceEpoch: "work-source"}, nil
	}}
	f, p, b, s := workFixture(t, port)
	w := preparedFixtureWork(t, f, p, b, s)
	after, err := p.ProduceWork(t.Context(), f.cred, w.ID)
	if err != nil || after.State != "native_ended" || after.Observed.NativeTurnID != "" || after.ID == "" {
		t.Fatalf("generic terminal required a Codex ID: %+v %v", after, err)
	}
	current, _, slot := readWorkResource(t, f, b)
	if current.CleanupStopped || slot.WorkID != "" {
		t.Fatal("RPC terminal invented process stop")
	}
}

func TestWorkExecuteActionAndPrivateMessageAreSeparateFromSessionControl(t *testing.T) {
	port := &workFixturePort{}
	f, p, task, run := bindingFixture(t, port)
	b := prepareFixture(t, f, p, task, run)
	b, err := p.Produce(t.Context(), f.cred, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	s, err := p.OpenEventStream(t.Context(), f.cred, b.ID, SourceStamp{Epoch: "work-source", Evidence: "native_sequence"})
	if err != nil {
		t.Fatal(err)
	}
	request := PrepareWorkRequest{Key: "work", BindingID: b.ID, StreamID: s.ID, Generation: b.Generation, ExpectedVersion: b.Version, Text: "private-work-text-sentinel"}
	if _, err = p.PrepareWork(t.Context(), f.cred, request); !errors.Is(err, ErrForbidden) {
		t.Fatal("session control acquired execute", err)
	}
	f.config.Actions = append(f.config.Actions, StartWork)
	if _, err = f.manager.ConfigureGrant(t.Context(), f.config, 2); err != nil {
		t.Fatal(err)
	}
	if _, err = p.PrepareWork(t.Context(), f.cred, request); !errors.Is(err, ErrForbidden) || port.starts.Load() != 0 {
		t.Fatal("new action upgraded an old Run", err)
	}
	// An independently registered executable Run has the explicit action.
	f, p, b, s = workFixture(t, &workFixturePort{})
	w := preparedFixtureWork(t, f, p, b, s)
	raw, err := json.Marshal(w)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("explicit-private-work")) {
		t.Fatal("private message entered the work receipt")
	}
	db, err := sql.Open("sqlite", f.path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var stored int
	if err = db.QueryRow(`SELECT count(*) FROM records WHERE kind='runtime.work-input' AND CAST(data AS TEXT) LIKE '%explicit-private-work%'`).Scan(&stored); err != nil || stored != 1 {
		t.Fatalf("private explicit message was not retained: %d %v", stored, err)
	}
	for _, query := range []string{`SELECT count(*) FROM audit WHERE CAST(data AS TEXT) LIKE '%explicit-private-work%'`,
		`SELECT count(*) FROM idempotency WHERE CAST(response AS TEXT) LIKE '%explicit-private-work%'`} {
		var count int
		if err = db.QueryRow(query).Scan(&count); err != nil || count != 0 {
			t.Fatalf("private message leaked: %d %v", count, err)
		}
	}
}
