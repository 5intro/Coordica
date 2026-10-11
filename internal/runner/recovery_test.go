package runner

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"coordica/internal/domain"
)

func uncertainRecord() spool {
	record := testRecord()
	record.Acknowledged = true
	record.Request = &domain.CompleteRequest{Generation: record.Assignment.Run.Generation, Status: "stopped", Quiescent: false, Detail: "prior process is unknown"}
	return record
}

func TestRestartPreservesDurableQuietCompletionDespiteLostCenterACK(t *testing.T) {
	record := testRecord()
	record.Acknowledged = true
	record.PID = 0 // The durable tree-stop observation is stronger than a missing PID.
	want, err := json.Marshal(record.Request)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != "POST" || req.URL.Path != "/api/v1/runs/run_test/complete" {
			t.Errorf("durable quiet result was replaced by reconciliation: %s %s", req.Method, req.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		raw, _ := io.ReadAll(req.Body)
		if string(raw) != string(want) || req.Header.Get("Idempotency-Key") != record.Key {
			t.Errorf("original quiet completion changed: key=%s request=%s", req.Header.Get("Idempotency-Key"), raw)
		}
		mu.Lock()
		requests++
		first := requests == 1
		mu.Unlock()
		if first {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = conn.Close()
			return
		}
		respond(w, domain.Run{ID: "run_test", Generation: 7, State: "succeeded", Quiescent: true})
	}))
	defer server.Close()
	r := testRunner(t, server)
	r.OnEvent = func(Event) {}
	if err := writePrivateJSON(r.spoolPath("run_test"), record); err != nil {
		t.Fatal(err)
	}
	if err := r.recoverRecords(context.Background()); err != nil {
		t.Fatal(err)
	}
	var kept spool
	if err := readJSON(r.spoolPath("run_test"), &kept); err != nil || kept.Key != record.Key || kept.Acknowledged || kept.Request == nil || !kept.Request.Quiescent || kept.Request.Status != record.Request.Status {
		t.Fatalf("lost center acknowledgement erased durable stop evidence: record=%+v err=%v", kept, err)
	}
	restarted, err := New(r.Config())
	if err != nil {
		t.Fatal(err)
	}
	restarted.OnEvent = func(Event) {}
	if err := restarted.recoverRecords(context.Background()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	count := requests
	mu.Unlock()
	if count != 2 {
		t.Fatalf("quiet handoff was not retried once: %d", count)
	}
	if _, err := os.Stat(r.spoolPath("run_test")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("acknowledged durable quiet handoff was not removed")
	}
}

func TestPeriodicRecoveryKeepsMissingIdentityBlocked(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { requests++ }))
	defer server.Close()
	r := testRunner(t, server)
	r.OnEvent = func(Event) {}
	record := uncertainRecord()
	if err := writePrivateJSON(r.spoolPath(record.Assignment.Run.ID), record); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		r.flush(context.Background())
	}
	var kept spool
	if err := readJSON(r.spoolPath(record.Assignment.Run.ID), &kept); err != nil {
		t.Fatal(err)
	}
	if requests != 0 || kept.Key != record.Key || kept.Request.Quiescent || !kept.Acknowledged {
		t.Fatalf("missing identity was released: requests=%d record=%+v", requests, kept)
	}
	if count, err := r.occupiedSlots(); err != nil || count != 1 {
		t.Fatalf("uncertain record lost its slot: count=%d err=%v", count, err)
	}
}

func TestPeriodicRecoveryLeavesActiveHandoffsAlone(t *testing.T) {
	for _, acknowledged := range []bool{false, true} {
		t.Run(map[bool]string{false: "unacknowledged", true: "acknowledged"}[acknowledged], func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { requests++ }))
			defer server.Close()
			r := testRunner(t, server)
			record := uncertainRecord()
			record.Acknowledged = acknowledged
			if err := writePrivateJSON(r.spoolPath(record.Assignment.Run.ID), record); err != nil {
				t.Fatal(err)
			}
			r.active[record.Assignment.Run.ID] = true
			r.flush(context.Background())
			var kept spool
			if err := readJSON(r.spoolPath(record.Assignment.Run.ID), &kept); err != nil {
				t.Fatal(err)
			}
			if requests != 0 || kept.Key != record.Key || kept.Request.Quiescent || kept.Acknowledged != acknowledged {
				t.Fatal("periodic recovery competed with an active worker")
			}
		})
	}
}

func TestPeriodicRecoveryRetriesUnacknowledgedUnknownRequestUnchanged(t *testing.T) {
	record := uncertainRecord()
	record.Acknowledged = false
	want, err := json.Marshal(record.Request)
	if err != nil {
		t.Fatal(err)
	}
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		requests++
		raw, _ := io.ReadAll(req.Body)
		if req.Method != "POST" || string(raw) != string(want) || req.Header.Get("Idempotency-Key") != record.Key {
			t.Errorf("fixed unknown handoff changed: %s %s %s", req.Method, req.Header.Get("Idempotency-Key"), raw)
		}
		respond(w, domain.Run{ID: record.Assignment.Run.ID, Generation: record.Assignment.Run.Generation, State: "lost"})
	}))
	defer server.Close()
	r := testRunner(t, server)
	if err := writePrivateJSON(r.spoolPath(record.Assignment.Run.ID), record); err != nil {
		t.Fatal(err)
	}
	r.flush(context.Background())
	var kept spool
	if err := readJSON(r.spoolPath(record.Assignment.Run.ID), &kept); err != nil {
		t.Fatal(err)
	}
	if requests != 1 || kept.Key != record.Key || kept.Request.Quiescent || !kept.Acknowledged {
		t.Fatal("unknown fixed handoff was not preserved and acknowledged")
	}
}
