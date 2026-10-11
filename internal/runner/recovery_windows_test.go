//go:build windows

package runner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"coordica/internal/domain"
)

func TestPeriodicWindowsRecoveryDoesNotInferJobQuiescenceFromParent(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { requests++ }))
	defer server.Close()
	r := testRunner(t, server)
	r.OnEvent = func(Event) {}
	record := uncertainRecord()
	record.PID = 2147483647 // A missing parent alone cannot prove the Job tree stopped.
	if err := writePrivateJSON(r.spoolPath(record.Assignment.Run.ID), record); err != nil {
		t.Fatal(err)
	}
	r.flush(context.Background())
	var kept spool
	if err := readJSON(r.spoolPath(record.Assignment.Run.ID), &kept); err != nil {
		t.Fatal(err)
	}
	if requests != 0 || kept.Key != record.Key || kept.Request.Quiescent || !kept.Acknowledged {
		t.Fatal("missing Windows parent was treated as confirmed Job quiescence")
	}
}

func TestWindowsRestartKeepsUnknownJobTreeBlocked(t *testing.T) {
	for _, existingRequest := range []bool{false, true} {
		t.Run(map[bool]string{false: "unfinished record", true: "acknowledged uncertain handoff"}[existingRequest], func(t *testing.T) {
			completions := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.Method == "GET" {
					respond(w, domain.Run{ID: "run_test", Generation: 7, State: "lost"})
					return
				}
				completions++
				var value domain.CompleteRequest
				if err := json.NewDecoder(req.Body).Decode(&value); err != nil {
					t.Error(err)
				}
				if value.Quiescent {
					t.Error("restart inferred Job quiescence from a missing parent")
				}
				respond(w, domain.Run{ID: "run_test", Generation: 7, State: "lost"})
			}))
			defer server.Close()
			r := testRunner(t, server)
			r.OnEvent = func(Event) {}
			record := uncertainRecord()
			record.PID = 2147483647
			if !existingRequest {
				record.Request = nil
				record.Acknowledged = false
			}
			if err := writePrivateJSON(r.spoolPath(record.Assignment.Run.ID), record); err != nil {
				t.Fatal(err)
			}
			if err := r.recoverRecords(context.Background()); err != nil {
				t.Fatal(err)
			}
			var kept spool
			if err := readJSON(r.spoolPath(record.Assignment.Run.ID), &kept); err != nil || kept.Request == nil || kept.Request.Quiescent || !kept.Acknowledged {
				t.Fatalf("unknown Windows tree did not remain durable: %v", err)
			}
			r.flush(context.Background())
			if completions != 1 {
				t.Fatalf("restart generated repeated unknown handoffs: %d", completions)
			}
			if count, err := r.occupiedSlots(); err != nil || count != 1 {
				t.Fatalf("unknown Windows Job freed capacity: %d %v", count, err)
			}
		})
	}
}
