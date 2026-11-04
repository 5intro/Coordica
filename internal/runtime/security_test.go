package runtime

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func TestApplicationAndTargetScopesCannotBeClaimedOrRebound(t *testing.T) {
	f := fixtureLedger(t)
	task := submitFixture(t, f, "submit")
	other := f.config
	other.ID, other.ApplicationID, other.Token = "principal-b", "application-b", strings.Repeat("other-fixture-token", 3)
	if _, err := f.manager.ConfigureGrant(t.Context(), other, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ledger.ReadTask(t.Context(), Credential{PrincipalID: other.ID, Token: other.Token}, task.Task.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("cross-application task access allowed: %v", err)
	}
	bad := f.config
	bad.ApplicationID = other.ApplicationID
	if _, err := f.manager.ConfigureGrant(t.Context(), bad, 1); !errors.Is(err, ErrConflict) {
		t.Fatalf("principal was rebound to another application: %v", err)
	}
	for _, change := range []func(*Instance){func(i *Instance) { i.ApplicationID = "another-app" },
		func(i *Instance) { i.NodeID = "another-node" }, func(i *Instance) { i.Harness = "another-harness" }} {
		instance := Instance{ID: "instance-a", ApplicationID: "application-a", NodeID: "node-a", Harness: "codex",
			ConfigRef: "changed-config", Support: "unknown", Enabled: true}
		change(&instance)
		if _, err := f.manager.ConfigureInstance(t.Context(), instance, 1); !errors.Is(err, ErrConflict) {
			t.Fatalf("instance identity was rebound: %+v %v", instance, err)
		}
	}
	badTarget := f.target
	badTarget.NodeID = "ungranted-node"
	if _, err := f.ledger.RegisterRun(t.Context(), f.cred, RegisterRunRequest{Key: "wrong-node", TaskID: task.Task.ID,
		ExpectedVersion: 1, ExpectedContextVersion: 1, Target: badTarget}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("caller-selected node bypassed persisted scope: %v", err)
	}
}

func TestInstanceVersionAndGrantRevocationPrecedeRunReplay(t *testing.T) {
	f := fixtureLedger(t)
	task := submitFixture(t, f, "submit")
	request := RegisterRunRequest{Key: "run", TaskID: task.Task.ID, ExpectedVersion: 1, ExpectedContextVersion: 1, Target: f.target}
	before, err := f.ledger.RegisterRun(t.Context(), f.cred, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.manager.ConfigureInstance(t.Context(), Instance{ID: "instance-a", ApplicationID: "application-a",
		NodeID: "node-a", Harness: "codex", ConfigRef: "fixture-config-v2", Support: "conditional", Enabled: true}, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ledger.RegisterRun(t.Context(), f.cred, request); !errors.Is(err, ErrConflict) {
		t.Fatalf("cached receipt ignored current instance configuration: %v", err)
	}
	old, err := f.ledger.ReadRun(t.Context(), f.cred, task.Task.ID, before.Run.ID)
	if err != nil || old.Run.InstanceConfigRef != "fixture-config-v1" || old.Run.Target.InstanceVersion != 1 {
		t.Fatalf("configuration update rewrote old run: %+v %v", old, err)
	}
	f.config.Targets = nil
	if _, err := f.manager.ConfigureGrant(t.Context(), f.config, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ledger.ReadRun(t.Context(), f.cred, task.Task.ID, before.Run.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("revoked target scope remained readable: %v", err)
	}
	if _, err := f.ledger.RegisterRun(t.Context(), f.cred, request); !errors.Is(err, ErrForbidden) {
		t.Fatalf("cached receipt ignored revoked target scope: %v", err)
	}
}

func TestRuntimeRecordsAndAuditDoNotExposeCredentialsOrLegacyTasks(t *testing.T) {
	f := fixtureLedger(t)
	private := "private-context-fixture-material"
	if _, err := f.ledger.SubmitTask(t.Context(), f.cred, SubmitTaskRequest{Key: "submit", Title: "Private title", Context: private}); err != nil {
		t.Fatal(err)
	}
	err := f.db.View(t.Context(), func(tx *store.Tx) error {
		oldTasks, err := store.List[domain.Task](tx, "task")
		if err != nil || len(oldTasks) != 0 {
			t.Fatalf("runtime entered legacy task namespace: %d %v", len(oldTasks), err)
		}
		oldRuns, err := store.List[domain.Run](tx, "run")
		if err != nil || len(oldRuns) != 0 {
			t.Fatalf("runtime entered legacy run namespace: %d %v", len(oldRuns), err)
		}
		events, err := tx.Events(100)
		if err != nil {
			return err
		}
		for _, event := range events {
			raw, _ := json.Marshal(event)
			if strings.Contains(string(raw), f.cred.Token) || strings.Contains(string(raw), private) || event.Detail != "" {
				t.Fatalf("global audit leaked private runtime material: %+v", event)
			}
		}
		var record grantRecord
		if err := tx.Get(kindGrant, f.cred.PrincipalID, &record); err != nil {
			return err
		}
		raw, _ := json.Marshal(record)
		if strings.Contains(string(raw), f.cred.Token) || len(record.TokenHash) != 32 {
			t.Fatal("grant stored plaintext token or invalid hash")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
