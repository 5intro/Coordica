package runtime

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"coordica/internal/domain"
)

func TestRunOptionalSelectionsPersistReplayAndReopenWithoutDefaults(t *testing.T) {
	for _, selected := range []struct{ name, model, effort string }{
		{"unspecified", "", ""}, {"model-only", "explicit/model:1", ""},
		{"effort-only", "", "provider-effort"}, {"both-explicit", "explicit/model:1", "provider-effort"},
	} {
		t.Run(selected.name, func(t *testing.T) {
			f := fixtureLedger(t)
			task := submitFixture(t, f, "task")
			target := f.target
			target.Model, target.Effort = selected.model, selected.effort
			request := RegisterRunRequest{Key: "run", TaskID: task.Task.ID, ExpectedVersion: 1, ExpectedContextVersion: 1, Target: target}
			first, err := f.ledger.RegisterRun(t.Context(), f.cred, request)
			if err != nil || first.Run.Target != target || first.Run.State != "registered" || first.Run.NativeDisposition != "not_started" {
				t.Fatalf("valid optional selection rejected or defaulted: %+v %v", first, err)
			}
			before := storageCounts(t, f.path)
			for attempt := 0; attempt < 2; attempt++ {
				if attempt == 1 {
					reopenFixture(t, &f)
				}
				read, err := f.ledger.ReadRun(t.Context(), f.cred, task.Task.ID, first.Run.ID)
				if err != nil || !reflect.DeepEqual(read, first) {
					t.Fatalf("snapshot changed: %+v %v", read, err)
				}
				replay, err := f.ledger.RegisterRun(t.Context(), f.cred, request)
				if err != nil || !reflect.DeepEqual(replay, first) {
					t.Fatalf("replay changed optional values: %+v %v", replay, err)
				}
			}
			changed := request
			changed.Target.Model = "different-explicit-model"
			var conflict *domain.Error
			if _, err := f.ledger.RegisterRun(t.Context(), f.cred, changed); !errors.As(err, &conflict) || conflict.Code != "idempotency_conflict" {
				t.Fatalf("same key changed model accepted: %v", err)
			}
			changed = request
			changed.Target.Effort = "different-explicit-effort"
			if _, err := f.ledger.RegisterRun(t.Context(), f.cred, changed); !errors.As(err, &conflict) || conflict.Code != "idempotency_conflict" {
				t.Fatalf("same key changed effort accepted: %v", err)
			}
			if after := storageCounts(t, f.path); !reflect.DeepEqual(before, after) {
				t.Fatal("replay/conflict wrote data", before, after)
			}
		})
	}
}

func TestRunOptionalSelectionsRejectInvalidNonemptyValuesAndRequiredIdentity(t *testing.T) {
	invalid := []struct {
		name string
		edit func(*Target)
	}{}
	for _, field := range []string{"model", "effort"} {
		for _, value := range []struct{ name, text string }{{"whitespace", " "}, {"control", "bad\nvalue"}, {"utf8", string([]byte{0xff})}, {"overlength", strings.Repeat("x", 129)}} {
			invalid = append(invalid, struct {
				name string
				edit func(*Target)
			}{field + "/" + value.name, func(target *Target) {
				if field == "model" {
					target.Model = value.text
				} else {
					target.Effort = value.text
				}
			}})
		}
	}
	invalid = append(invalid, []struct {
		name string
		edit func(*Target)
	}{
		{"missing-instance", func(v *Target) { v.InstanceID = "" }}, {"missing-node", func(v *Target) { v.NodeID = "" }},
		{"missing-harness", func(v *Target) { v.Harness = "" }}, {"missing-version", func(v *Target) { v.InstanceVersion = 0 }},
		{"missing-workspace", func(v *Target) { v.WorkspaceRef = "" }},
	}...)
	for _, scenario := range invalid {
		t.Run(scenario.name, func(t *testing.T) {
			f := fixtureLedger(t)
			task := submitFixture(t, f, "task")
			target := f.target
			target.Model, target.Effort = "", ""
			scenario.edit(&target)
			before := storageCounts(t, f.path)
			if _, err := f.ledger.RegisterRun(t.Context(), f.cred, RegisterRunRequest{Key: "invalid", TaskID: task.Task.ID, ExpectedVersion: 1, ExpectedContextVersion: 1, Target: target}); !errors.Is(err, ErrInvalid) {
				t.Fatal(err)
			}
			if after := storageCounts(t, f.path); !reflect.DeepEqual(before, after) {
				t.Fatal("invalid target wrote data", before, after)
			}
		})
	}
}

func TestRunOptionalSelectionsKeepCurrentScopeAndAuthorizationBeforeReplay(t *testing.T) {
	f := fixtureLedger(t)
	task := submitFixture(t, f, "task")
	target := f.target
	target.Model, target.Effort = "", ""
	request := RegisterRunRequest{Key: "run", TaskID: task.Task.ID, ExpectedVersion: 1, ExpectedContextVersion: 1, Target: target}
	bad := request
	bad.Target.NodeID = "ungranted-node"
	before := storageCounts(t, f.path)
	if _, err := f.ledger.RegisterRun(t.Context(), f.cred, bad); !errors.Is(err, ErrForbidden) {
		t.Fatalf("optional selection bypassed scope: %v", err)
	}
	if !reflect.DeepEqual(before, storageCounts(t, f.path)) {
		t.Fatal("scope denial wrote data")
	}
	first, err := f.ledger.RegisterRun(t.Context(), f.cred, request)
	if err != nil {
		t.Fatal(err)
	}
	f.config.Enabled = false
	if _, err := f.manager.ConfigureGrant(t.Context(), f.config, 1); err != nil {
		t.Fatal(err)
	}
	reopenFixture(t, &f)
	before = storageCounts(t, f.path)
	if _, err := f.ledger.RegisterRun(t.Context(), f.cred, request); !errors.Is(err, ErrForbidden) {
		t.Fatalf("revoked caller read receipt: %v", err)
	}
	if _, err := f.ledger.ReadRun(t.Context(), f.cred, task.Task.ID, first.Run.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("revoked caller read optional target: %v", err)
	}
	if !reflect.DeepEqual(before, storageCounts(t, f.path)) {
		t.Fatal("revoked replay wrote data")
	}
}
