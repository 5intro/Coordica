package runtimecodex

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"coordica/internal/agent/appserver"
	"coordica/internal/runtime"
	"coordica/internal/store"
)

func readBinding(t *testing.T, f *fixture) runtime.Binding {
	t.Helper()
	var b runtime.Binding
	if err := f.db.View(t.Context(), func(tx *store.Tx) error { return tx.Get("runtime.binding", f.binding.ID, &b) }); err != nil {
		t.Fatal(err)
	}
	return b
}

func journalCount(t *testing.T, f *fixture, method string) int {
	t.Helper()
	raw, err := os.ReadFile(f.journal)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte("\n")) {
		var frame struct{ Method string }
		if json.Unmarshal(line, &frame) == nil && frame.Method == method {
			count++
		}
	}
	return count
}

func awaitMethod(t *testing.T, f *fixture, method string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for journalCount(t, f, method) == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("request never written: %s", method)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func assertPrivateHistoryNotPersisted(t *testing.T, f *fixture) {
	t.Helper()
	db, err := sql.Open("sqlite", f.path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, q := range []string{"SELECT count(*) FROM records WHERE CAST(data AS TEXT) LIKE '%private-history-sentinel%'",
		"SELECT count(*) FROM audit WHERE CAST(data AS TEXT) LIKE '%private-history-sentinel%'",
		"SELECT count(*) FROM idempotency WHERE CAST(response AS TEXT) LIKE '%private-history-sentinel%'"} {
		var count int
		if err := db.QueryRow(q).Scan(&count); err != nil || count != 0 {
			t.Fatalf("private native history persisted: %d %v", count, err)
		}
	}
}

func TestResumeFailuresKeepOriginalNativeTargetAndNeverAutomaticallyRetry(t *testing.T) {
	for _, scenario := range []string{"resume-disconnect", "resume-model-mismatch", "resume-wrong-id", "resume-rejected"} {
		t.Run(scenario, func(t *testing.T) {
			f := setup(t, scenario)
			ready := produce(t, f)
			b := release(t, f, ready)
			req := runtime.BindingRequest{Key: "resume", BindingID: b.ID, ExpectedVersion: b.Version, Generation: b.Generation}
			_, err := f.producer.ResumeBinding(t.Context(), f.cred, req)
			if err == nil {
				t.Fatal("failed native resume was accepted")
			}
			got := readBinding(t, f)
			if got.NativeID != ready.NativeID {
				t.Fatalf("resume observation replaced authoritative target: %+v", got)
			}
			var command runtime.BindingCommand
			if err := f.db.View(t.Context(), func(tx *store.Tx) error { return tx.Get("runtime.binding-command", got.CommandID, &command) }); err != nil {
				t.Fatal(err)
			}
			if scenario == "resume-rejected" {
				var rpc *appserver.RPCError
				if !errors.As(err, &rpc) || errors.Is(err, runtime.ErrOutcomeUnknown) || got.State != "released" || !command.Stop.TreeStopped {
					t.Fatalf("definite rejection misclassified: %+v %v", got, err)
				}
			} else {
				if !errors.Is(err, runtime.ErrOutcomeUnknown) || got.State != "unknown" || !got.CleanupStopped {
					t.Fatalf("uncertain resume lacked fence/stop: %+v %v", got, err)
				}
				if scenario == "resume-model-mismatch" && (command.Observed.ID != ready.NativeID || !bytes.Contains(command.Observed.Configuration, []byte("different-model"))) {
					t.Fatalf("allocated identity/config observation lost: %+v", command)
				}
				if scenario == "resume-wrong-id" && command.Observed.ID.Value != "wrong-native-thread" {
					t.Fatalf("wrong identity observation missing: %+v", command)
				}
			}
			if _, err := f.producer.ResumeBinding(t.Context(), f.cred, req); err == nil {
				t.Fatal("same command repeated")
			}
			if journalCount(t, f, "thread/resume") != 1 || journalCount(t, f, "thread/start") != 1 {
				t.Fatal("failed resume replayed native operation")
			}
		})
	}
}

func TestConcurrentResumeSingleWinnerAndUnknownCannotTakeOver(t *testing.T) {
	f := setup(t, "")
	ready := produce(t, f)
	if _, err := f.producer.ResumeBinding(t.Context(), f.cred, runtime.BindingRequest{Key: "unsafe", BindingID: ready.ID, ExpectedVersion: ready.Version, Generation: ready.Generation}); !errors.Is(err, runtime.ErrStopUnconfirmed) {
		t.Fatal(err)
	}
	b := release(t, f, ready)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.producer.ResumeBinding(t.Context(), f.cred, runtime.BindingRequest{Key: "same-resume", BindingID: b.ID, ExpectedVersion: b.Version, Generation: b.Generation})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	success := 0
	for err := range errs {
		if err == nil {
			success++
		}
	}
	if success != 1 || journalCount(t, f, "thread/resume") != 1 {
		t.Fatalf("duplicate resume: winners=%d count=%d", success, journalCount(t, f, "thread/resume"))
	}
	current := readBinding(t, f)
	if _, err := f.producer.RecoverUnknown(t.Context(), f.cred, runtime.BindingRequest{Key: "recover", BindingID: current.ID, ExpectedVersion: current.Version, Generation: current.Generation}); err != nil {
		t.Fatal(err)
	}
	unknown := readBinding(t, f)
	if _, err := f.producer.ResumeBinding(t.Context(), f.cred, runtime.BindingRequest{Key: "no-takeover", BindingID: unknown.ID, ExpectedVersion: unknown.Version, Generation: unknown.Generation}); !errors.Is(err, runtime.ErrStopUnconfirmed) {
		t.Fatal(err)
	}
}

func TestNativeReadExplicitRejectionAndPreCancelKeepReadyPeer(t *testing.T) {
	f := setup(t, "read-rejected")
	ready := produce(t, f)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := f.producer.ReadNative(ctx, f.cred, ready.ID, true); !errors.Is(err, context.Canceled) || journalCount(t, f, "thread/read") != 0 {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		_, err := f.producer.ReadNative(t.Context(), f.cred, ready.ID, true)
		var rpc *appserver.RPCError
		if !errors.As(err, &rpc) || errors.Is(err, runtime.ErrOutcomeUnknown) {
			t.Fatalf("read rejection misclassified: %v", err)
		}
	}
	if readBinding(t, f) != ready || journalCount(t, f, "thread/read") != 2 {
		t.Fatal("explicit rejection invalidated live binding")
	}
}

func TestNativeReadLateSuccessAndErrorDoNotLeakAfterRevocationOrRecovery(t *testing.T) {
	for _, scenario := range []string{"read-pause", "read-error-pause"} {
		for _, change := range []string{"revoke", "recover"} {
			t.Run(scenario+"/"+change, func(t *testing.T) {
				f := setup(t, scenario)
				ready := produce(t, f)
				type result struct {
					raw json.RawMessage
					err error
				}
				done := make(chan result, 1)
				go func() {
					raw, err := f.producer.ReadNative(t.Context(), f.cred, ready.ID, true)
					done <- result{raw, err}
				}()
				awaitMethod(t, f, "thread/read")
				var recovered runtime.Binding
				if change == "revoke" {
					f.grant.Enabled = false
					if _, err := f.manager.ConfigureGrant(t.Context(), f.grant, 1); err != nil {
						t.Fatal(err)
					}
				} else {
					var err error
					recovered, err = f.producer.RecoverUnknown(t.Context(), f.cred, runtime.BindingRequest{Key: "recover", BindingID: ready.ID, ExpectedVersion: ready.Version, Generation: ready.Generation})
					if err != nil {
						t.Fatal(err)
					}
				}
				if err := os.WriteFile(f.proceed, []byte("go"), 0600); err != nil {
					t.Fatal(err)
				}
				got := <-done
				var rpc *appserver.RPCError
				if len(got.raw) != 0 || !errors.Is(got.err, runtime.ErrOutcomeUnknown) || errors.As(got.err, &rpc) || bytes.Contains([]byte(got.err.Error()), []byte("private-history-sentinel")) {
					t.Fatalf("late private result escaped: %q %v", got.raw, got.err)
				}
				if change == "recover" && readBinding(t, f) != recovered {
					t.Fatal("late reader advanced recovered generation")
				}
				assertPrivateHistoryNotPersisted(t, f)
			})
		}
	}
}

func TestWrittenReadCancellationAndDisconnectQuarantineWithoutReopen(t *testing.T) {
	for _, scenario := range []string{"read-pause", "read-disconnect"} {
		t.Run(scenario, func(t *testing.T) {
			f := setup(t, scenario)
			ready := produce(t, f)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := f.producer.ReadNative(ctx, f.cred, ready.ID, true); done <- err }()
			awaitMethod(t, f, "thread/read")
			if scenario == "read-pause" {
				cancel()
			}
			if err := <-done; !errors.Is(err, runtime.ErrOutcomeUnknown) {
				t.Fatal(err)
			}
			unknown := readBinding(t, f)
			if unknown.State != "unknown" || !unknown.CleanupStopped || unknown.NativeID != ready.NativeID {
				t.Fatal(unknown)
			}
			if _, err := f.producer.ReadNative(t.Context(), f.cred, ready.ID, true); err == nil {
				t.Fatal("read reopened unknown binding")
			}
			if journalCount(t, f, "initialize") != 1 || journalCount(t, f, "thread/read") != 1 {
				t.Fatal("read replayed a peer or RPC")
			}
		})
	}
}

func TestResumePrepareRollbackHasNoRPCAndForeignNativeIndexCannotBeRebound(t *testing.T) {
	for _, scenario := range []string{"receipt-rollback", "foreign-index"} {
		t.Run(scenario, func(t *testing.T) {
			f := setup(t, "")
			ready := produce(t, f)
			released := release(t, f, ready)
			db, err := sql.Open("sqlite", f.path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			query := `CREATE TRIGGER fixture_fail_resume_receipt BEFORE INSERT ON idempotency BEGIN SELECT RAISE(ABORT,'fixture receipt failure'); END`
			if scenario == "foreign-index" {
				query = `UPDATE records SET data='"foreign-binding"' WHERE kind='runtime.binding-native'`
			}
			if _, err := db.Exec(query); err != nil {
				t.Fatal(err)
			}
			_, err = f.producer.ResumeBinding(t.Context(), f.cred, runtime.BindingRequest{Key: "resume", BindingID: released.ID, ExpectedVersion: released.Version, Generation: released.Generation})
			if err == nil {
				t.Fatal("invalid resume committed")
			}
			if scenario == "receipt-rollback" {
				if readBinding(t, f) != released || journalCount(t, f, "thread/resume") != 0 {
					t.Fatal("rolled-back resume issued native I/O or changed binding")
				}
			} else {
				if !errors.Is(err, runtime.ErrOutcomeUnknown) || readBinding(t, f).NativeID != ready.NativeID {
					t.Fatal("foreign native occupancy lost fence")
				}
				var data string
				if err := db.QueryRow(`SELECT data FROM records WHERE kind='runtime.binding-native' LIMIT 1`).Scan(&data); err != nil || data != `"foreign-binding"` {
					t.Fatalf("foreign index overwritten: %s %v", data, err)
				}
			}
		})
	}
}

func TestInitializeFailureDoesNotInventStopProofFromNilPeer(t *testing.T) {
	f := setup(t, "initialize-rejected")
	if _, err := f.producer.Produce(t.Context(), f.cred, f.binding.ID); !errors.Is(err, runtime.ErrOutcomeUnknown) {
		t.Fatal(err)
	}
	b := readBinding(t, f)
	if b.State != "unknown" || b.CleanupStopped || journalCount(t, f, "thread/start") != 0 {
		t.Fatalf("nil peer invented stopping evidence: %+v", b)
	}
}

type privateStopError struct {
	runtime.SessionPort
	fail bool
}

func (p *privateStopError) Stop(ctx context.Context, call runtime.NativeCreate, id runtime.NativeSessionID) (runtime.BindingStop, error) {
	proof, err := p.SessionPort.Stop(ctx, call, id)
	if p.fail {
		return runtime.BindingStop{}, &appserver.RPCError{Code: -32001, Message: "private-cleanup-sentinel", Data: json.RawMessage(`{"private":"private-cleanup-sentinel"}`)}
	}
	return proof, err
}

func TestExpiredControlErrorsDoNotExposeNativeOrCleanupPrivateData(t *testing.T) {
	for _, scenario := range []string{"resume-proven-stop-revoke", "resume-proven-stop-recover", "initialize-no-peer-revoke", "resume-private-stop-revoke"} {
		t.Run(scenario, func(t *testing.T) {
			initializing := scenario == "initialize-no-peer-revoke"
			mode := "resume-error-pause"
			if initializing {
				mode = "initialize-error-pause"
			}
			f := setup(t, mode)
			wrapper := &privateStopError{SessionPort: f.port}
			var err error
			f.producer, err = runtime.NewBindingProducer(f.db, f.port.config, wrapper)
			if err != nil {
				t.Fatal(err)
			}
			var released runtime.Binding
			if !initializing {
				released = release(t, f, produce(t, f))
			}
			wrapper.fail = scenario == "resume-private-stop-revoke"
			done := make(chan error, 1)
			go func() {
				if initializing {
					_, err := f.producer.Produce(t.Context(), f.cred, f.binding.ID)
					done <- err
				} else {
					_, err := f.producer.ResumeBinding(t.Context(), f.cred, runtime.BindingRequest{Key: "private-resume", BindingID: released.ID, ExpectedVersion: released.Version, Generation: released.Generation})
					done <- err
				}
			}()
			method := "thread/resume"
			if initializing {
				method = "initialize"
			}
			awaitMethod(t, f, method)
			if scenario == "resume-proven-stop-recover" {
				current := readBinding(t, f)
				if _, err := f.producer.RecoverUnknown(t.Context(), f.cred, runtime.BindingRequest{Key: "recover", BindingID: current.ID, ExpectedVersion: current.Version, Generation: current.Generation}); err != nil {
					t.Fatal(err)
				}
			} else {
				f.grant.Enabled = false
				if _, err := f.manager.ConfigureGrant(t.Context(), f.grant, 1); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(f.proceed, []byte("go"), 0600); err != nil {
				t.Fatal(err)
			}
			err = <-done
			var rpc *appserver.RPCError
			if !errors.Is(err, runtime.ErrOutcomeUnknown) || errors.As(err, &rpc) || bytes.Contains([]byte(err.Error()), []byte("private-")) {
				t.Fatalf("expired caller received private native/cleanup error: %v RPC=%+v", err, rpc)
			}
		})
	}
}
