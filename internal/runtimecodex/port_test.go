package runtimecodex

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"

	"coordica/internal/runtime"
	"coordica/internal/store"
)

func TestSQLiteProtocolCreateReadReleaseResumeDoesNotSendWork(t *testing.T) {
	f := setup(t, "")
	ctx, cancel := context.WithCancel(t.Context())
	ready, err := f.producer.Produce(ctx, f.cred, f.binding.ID)
	if err != nil {
		t.Fatal(err)
	}
	cancel() // Successful operation context expiry does not end the host Peer.
	raw, err := f.producer.ReadNative(t.Context(), f.cred, ready.ID, true)
	if err != nil || !bytes.Contains(raw, []byte("private-history-sentinel")) {
		t.Fatalf("native read failed: %s %v", raw, err)
	}
	released := release(t, f, ready)
	if err := f.db.Close(); err != nil {
		t.Fatal(err)
	}
	f.db, err = store.Open(f.path)
	if err != nil {
		t.Fatal(err)
	}
	f.ledger, f.manager = runtime.New(f.db), runtime.NewManager(f.db)
	f.port, err = New(t.Context(), f.port.config, f.port.native)
	if err != nil {
		t.Fatal(err)
	}
	other, err := runtime.NewBindingProducer(f.db, f.port.config, f.port)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.ReadNative(t.Context(), f.cred, released.ID, true); err == nil || journalCount(t, f, "initialize") != 1 {
		t.Fatal("read/constructor reopened a peer")
	}
	req := runtime.BindingRequest{Key: "resume", BindingID: released.ID, ExpectedVersion: released.Version, Generation: released.Generation}
	resumed, err := other.ResumeBinding(t.Context(), f.cred, req)
	if err != nil || resumed.NativeID != ready.NativeID || resumed.Generation != ready.Generation+1 || resumed.OwnerID == ready.OwnerID {
		t.Fatalf("resume identity/fence: %+v %v", resumed, err)
	}
	if _, err := other.ReadNative(t.Context(), f.cred, resumed.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := other.ResumeBinding(t.Context(), f.cred, req); err == nil {
		t.Fatal("duplicate resume reported fresh side effect")
	}
	journal, err := os.ReadFile(f.journal)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, line := range bytes.Split(bytes.TrimSpace(journal), []byte("\n")) {
		var frame struct{ Method string }
		if json.Unmarshal(line, &frame) != nil {
			t.Fatal(string(line))
		}
		counts[frame.Method]++
	}
	if counts["thread/start"] != 1 || counts["thread/resume"] != 1 || counts["thread/read"] != 2 || counts["turn/start"] != 0 || bytes.Contains(journal, []byte("TASK-SECRET")) {
		t.Fatalf("unexpected native work: %v %s", counts, journal)
	}
	assertPrivateHistoryNotPersisted(t, f)
}
