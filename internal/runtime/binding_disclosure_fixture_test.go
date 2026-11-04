package runtime

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"coordica/internal/process"
	"coordica/internal/store"
)

// The private payload models a typed native/cleanup diagnostic. Error deliberately
// omits it, so checking Error() alone cannot detect an errors.As disclosure.
type bindingPrivateFault struct{ Operation, Data string }

func (e *bindingPrivateFault) Error() string { return e.Operation + " failed" }

type disclosureBindingPort struct {
	actual                            *processBindingPort
	transportOnly, unknown, loseProof bool
	createError, stopError            error
	afterCreate                       func(context.Context, NativeCreate) error
	stopEntered                       chan NativeCreate
	stopContinue                      chan struct{}
	actualStop                        BindingStop
}

func (p *disclosureBindingPort) Create(ctx context.Context, call NativeCreate) (NativeCreation, error) {
	var observed NativeCreation
	var err error
	if p.transportOnly {
		// Start a real owned transport, but send no fixture/native Session command.
		// This represents a known rejection with no native ID, not a fake stop.
		p.actual.creates.Add(1)
		executable, e := os.Executable()
		if e != nil {
			return NativeCreation{Outcome: "not_sent"}, e
		}
		env := []string{"COORDICA_BINDING_FIXTURE=1", "COORDICA_BINDING_JOURNAL=" + p.actual.journal, "GORACE=atexit_sleep_ms=0", "GOMAXPROCS=1"}
		for _, name := range []string{"PATH", "SystemRoot", "TMPDIR", "TMP", "TEMP"} {
			if value := os.Getenv(name); value != "" {
				env = append(env, name+"="+value)
			}
		}
		child, e := process.Start(p.actual.life, process.Spec{Executable: executable, Args: []string{"runtime-binding-fixture"}, Dir: p.actual.directory, Env: env, MaxLineBytes: 32 << 10})
		if e != nil {
			return NativeCreation{Outcome: "not_sent"}, e
		}
		p.actual.mu.Lock()
		p.actual.child, p.actual.call = child, call
		p.actual.mu.Unlock()
		observed = NativeCreation{Outcome: "rejected"}
	} else {
		observed, err = p.actual.Create(ctx, call)
		if err != nil {
			return observed, err
		}
	}
	if p.afterCreate != nil {
		if err = p.afterCreate(ctx, call); err != nil {
			return NativeCreation{Outcome: "unknown"}, err
		}
	}
	if p.unknown {
		observed.Outcome = "unknown"
	}
	return observed, p.createError
}

func (p *disclosureBindingPort) Stop(ctx context.Context, call NativeCreate, id NativeSessionID) (BindingStop, error) {
	if p.stopEntered != nil {
		select {
		case p.stopEntered <- call:
		case <-ctx.Done():
			return BindingStop{}, ctx.Err()
		}
		select {
		case <-p.stopContinue:
		case <-ctx.Done():
			return BindingStop{}, ctx.Err()
		}
	}
	p.actual.mu.Lock()
	started := p.actual.child != nil
	p.actual.mu.Unlock()
	if !started {
		return BindingStop{}, errors.New("disclosure fixture has no actual child")
	}
	proof, err := p.actual.Stop(ctx, call, id)
	p.actualStop = proof // TreeStopped is exclusively the actual process Wait result.
	if err != nil {
		return proof, err
	}
	if p.loseProof {
		return BindingStop{}, p.stopError
	} // Simulate losing the returned proof, even though the child stopped.
	return proof, p.stopError
}

func assertNoBindingPrivateError(t *testing.T, err error) {
	t.Helper()
	var private *bindingPrivateFault
	if errors.As(err, &private) {
		t.Errorf("private %s payload reachable through errors.As: %q", private.Operation, private.Data)
	}
	if err == nil || strings.Contains(err.Error(), "private-native-marker") || strings.Contains(err.Error(), "private-cleanup-marker") {
		t.Errorf("private error text or missing failure: %v", err)
	}
}
func readDisclosureRun(t *testing.T, f ledgerFixture, id string) Run {
	t.Helper()
	var run Run
	if err := f.db.View(t.Context(), func(tx *store.Tx) error { return tx.Get(kindRun, id, &run) }); err != nil {
		t.Fatal(err)
	}
	return run
}
func assertDisclosureNotInNotifications(t *testing.T, f ledgerFixture) {
	t.Helper()
	err := f.db.View(t.Context(), func(tx *store.Tx) error {
		events, err := tx.Events(200)
		if err != nil {
			return err
		}
		raw, _ := json.Marshal(events)
		if strings.Contains(string(raw), "private-native-marker") || strings.Contains(string(raw), "private-cleanup-marker") {
			t.Error("private diagnostic copied into global audit")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", f.path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query("SELECT response FROM idempotency")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "private-native-marker") || strings.Contains(string(raw), "private-cleanup-marker") {
			t.Error("private diagnostic copied into request receipt")
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}
