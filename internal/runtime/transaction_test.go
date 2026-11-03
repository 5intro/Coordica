package runtime

import (
	"database/sql"
	"errors"
	"reflect"
	"sync"
	"testing"
)

func storageCounts(t *testing.T, path string) map[string]int {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	counts := map[string]int{}
	for _, table := range []string{"records", "audit", "outbox", "idempotency"} {
		var count int
		if err := db.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		counts[table] = count
	}
	return counts
}

func TestConcurrentContextCASHasOneAtomicWinner(t *testing.T) {
	f := fixtureLedger(t)
	task := submitFixture(t, f, "submit")
	before := storageCounts(t, f.path)
	start := make(chan struct{})
	results := make(chan error, 2)
	var wait sync.WaitGroup
	for _, key := range []string{"one", "two"} {
		wait.Add(1)
		go func(key string) {
			defer wait.Done()
			<-start
			_, err := f.ledger.UpdateContext(t.Context(), f.cred, ContextRequest{Key: key, TaskID: task.Task.ID,
				ExpectedVersion: 1, ExpectedContextVersion: 1, Source: Source{ID: "program", EventID: key}, Content: key})
			results <- err
		}(key)
	}
	close(start)
	wait.Wait()
	close(results)
	winners, conflicts := 0, 0
	for err := range results {
		if err == nil {
			winners++
		} else if errors.Is(err, ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if winners != 1 || conflicts != 1 {
		t.Fatalf("lost update: winners=%d conflicts=%d", winners, conflicts)
	}
	after := storageCounts(t, f.path)
	if after["records"] != before["records"]+2 || after["audit"] != before["audit"]+1 ||
		after["outbox"] != before["outbox"]+1 || after["idempotency"] != before["idempotency"]+1 {
		t.Fatalf("failed CAS left partial records/audit/receipt: before=%v after=%v", before, after)
	}
}

func failReceiptWrites(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`CREATE TRIGGER fixture_fail_receipt BEFORE INSERT ON idempotency
 WHEN NEW.actor LIKE 'runtime:%' BEGIN SELECT RAISE(ABORT,'fixture receipt failure'); END`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestLateReceiptFailureRollsBackEntireLedgerTransaction(t *testing.T) {
	for _, operation := range []string{"submit", "context", "run", "continuation", "cancel-run", "cancel-task"} {
		t.Run(operation, func(t *testing.T) {
			f := fixtureLedger(t)
			task := submitFixture(t, f, "existing")
			var run RunSnapshot
			if operation == "continuation" || operation == "cancel-run" || operation == "cancel-task" {
				run = registerFixture(t, f, task, "existing-run")
				task = readFixture(t, f, task.Task.ID)
			}
			before := storageCounts(t, f.path)
			failReceiptWrites(t, f.path)
			err := rollbackOperation(t, f, task, run, operation)
			if err == nil {
				t.Fatal("injected final receipt failure did not fail request")
			}
			if after := storageCounts(t, f.path); !reflect.DeepEqual(before, after) {
				t.Fatalf("partial transaction survived: before=%v after=%v", before, after)
			}
			if after := readFixture(t, f, task.Task.ID); !reflect.DeepEqual(after, task) {
				t.Fatalf("failed transaction changed task: %+v", after)
			}
			if run.Run.ID != "" {
				after, err := f.ledger.ReadRun(t.Context(), f.cred, task.Task.ID, run.Run.ID)
				if err != nil || !reflect.DeepEqual(after, run) {
					t.Fatalf("failed transaction changed run: %+v %v", after, err)
				}
			}
		})
	}
}

func rollbackOperation(t *testing.T, f ledgerFixture, task TaskSnapshot, run RunSnapshot, operation string) error {
	t.Helper()
	switch operation {
	case "submit":
		_, err := f.ledger.SubmitTask(t.Context(), f.cred, SubmitTaskRequest{Key: "fail", Title: "New task"})
		return err
	case "context":
		_, err := f.ledger.UpdateContext(t.Context(), f.cred, ContextRequest{Key: "fail", TaskID: task.Task.ID,
			ExpectedVersion: task.Task.Version, ExpectedContextVersion: task.Task.ContextVersion, Source: Source{ID: "fixture", EventID: "fail"}, Content: "changed"})
		return err
	case "run":
		_, err := f.ledger.RegisterRun(t.Context(), f.cred, RegisterRunRequest{Key: "fail", TaskID: task.Task.ID,
			ExpectedVersion: task.Task.Version, ExpectedContextVersion: task.Task.ContextVersion, Target: f.target})
		return err
	case "continuation":
		_, err := f.ledger.RecordContinuation(t.Context(), f.cred, ContinuationRequest{Key: "fail", TaskID: task.Task.ID,
			ExpectedVersion: task.Task.Version, RunID: run.Run.ID, RunGeneration: run.Run.Generation,
			ContextVersion: run.Run.ContextVersion, Source: Source{ID: "fixture", EventID: "fail"}, Content: "changed"})
		return err
	case "cancel-run":
		_, err := f.ledger.CancelRun(t.Context(), f.cred, CancelRequest{Key: "fail", TaskID: task.Task.ID,
			ExpectedVersion: task.Task.Version, RunID: run.Run.ID, RunGeneration: run.Run.Generation})
		return err
	default:
		_, err := f.ledger.CancelTask(t.Context(), f.cred, CancelRequest{Key: "fail", TaskID: task.Task.ID, ExpectedVersion: task.Task.Version})
		return err
	}
}
