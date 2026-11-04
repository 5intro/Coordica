package runtime

import (
	"path/filepath"
	"strings"
	"testing"

	"coordica/internal/store"
)

type ledgerFixture struct {
	db      *store.Store
	ledger  *Ledger
	manager *Manager
	path    string
	config  GrantConfig
	cred    Credential
	target  Target
}

func fixtureLedger(t *testing.T) ledgerFixture {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ledger.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	f := ledgerFixture{db: db, ledger: New(db), manager: NewManager(db), path: path}
	f.config = GrantConfig{Grant: Grant{ID: "principal-a", ApplicationID: "application-a", Enabled: true,
		Actions: []Action{Submit, Read, WriteContext, Register, Continue, Cancel},
		Targets: []TargetScope{{InstanceID: "instance-a", NodeID: "node-a"}}}, Token: strings.Repeat("fixture-secret-", 4)}
	if _, err := f.manager.ConfigureGrant(t.Context(), f.config, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := f.manager.ConfigureInstance(t.Context(), Instance{ID: "instance-a", ApplicationID: "application-a",
		NodeID: "node-a", Harness: "codex", ConfigRef: "fixture-config-v1", Support: "unknown", Enabled: true}, 0); err != nil {
		t.Fatal(err)
	}
	f.cred = Credential{PrincipalID: f.config.ID, Token: f.config.Token}
	f.target = Target{InstanceID: "instance-a", InstanceVersion: 1, NodeID: "node-a", Harness: "codex",
		Model: "fixture-model", Effort: "ultra", WorkspaceRef: "ordinary-workspace"}
	return f
}

func submitFixture(t *testing.T, f ledgerFixture, key string) TaskSnapshot {
	t.Helper()
	task, err := f.ledger.SubmitTask(t.Context(), f.cred, SubmitTaskRequest{Key: key, Title: "Programmatic work", Context: "context-v1"})
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func reopenFixture(t *testing.T, f *ledgerFixture) {
	t.Helper()
	if err := f.db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(f.path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	f.db, f.ledger, f.manager = db, New(db), NewManager(db)
}

func TestLedgerPersistsTaskAndExplicitRunWithoutGitOrChat(t *testing.T) {
	f := fixtureLedger(t)
	task := submitFixture(t, f, "submit")
	if task.Task.CurrentRunID != "" || task.Task.State != "open" || task.Context.Version != 1 {
		t.Fatalf("submission fabricated execution: %+v", task)
	}
	run, err := f.ledger.RegisterRun(t.Context(), f.cred, RegisterRunRequest{Key: "run", TaskID: task.Task.ID,
		ExpectedVersion: 1, ExpectedContextVersion: 1, Target: f.target})
	if err != nil {
		t.Fatal(err)
	}
	if run.Run.State != "registered" || run.Run.NativeDisposition != "not_started" || run.Run.Generation != 1 {
		t.Fatalf("registration fabricated native execution: %+v", run)
	}
	reopenFixture(t, &f)
	got, err := f.ledger.ReadTask(t.Context(), f.cred, task.Task.ID)
	if err != nil || got.Task.CurrentRunID != run.Run.ID || got.Task.Version != 2 || got.Context.Content != "context-v1" {
		t.Fatalf("task failed restart recovery: %+v %v", got, err)
	}
	bound, err := f.ledger.ReadRun(t.Context(), f.cred, task.Task.ID, run.Run.ID)
	if err != nil || bound.Run != run.Run || bound.Context.Content != "context-v1" {
		t.Fatalf("run binding changed after reopen: %+v %v", bound, err)
	}
}
