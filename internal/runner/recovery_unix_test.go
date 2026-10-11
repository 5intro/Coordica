//go:build !windows

package runner

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"coordica/internal/domain"
	"coordica/internal/process"
)

// The root intentionally exits before its ordinary child. The recorded PID
// must keep representing the whole process group, not only the dead parent.
func TestRecoveryGroupHelper(t *testing.T) {
	role := os.Getenv("COORDICA_RECOVERY_HELPER_ROLE")
	if role == "" {
		return
	}
	dir := os.Getenv("COORDICA_RECOVERY_HELPER_DIR")
	if role == "parent" {
		exe, err := os.Executable()
		if err != nil {
			os.Exit(2)
		}
		child := exec.Command(exe, "-test.run=^TestRecoveryGroupHelper$")
		child.Env = append(os.Environ(), "COORDICA_RECOVERY_HELPER_ROLE=child")
		if err := child.Start(); err != nil {
			os.Exit(3)
		}
	} else if role == "child" {
		if err := os.WriteFile(filepath.Join(dir, "child-ready"), []byte("ready"), 0600); err != nil {
			os.Exit(4)
		}
	} else {
		os.Exit(5)
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(dir, role+"-release")); err == nil {
			os.Exit(0)
		}
		time.Sleep(10 * time.Millisecond)
	}
	os.Exit(6)
}

func waitRecoveryObservation(t *testing.T, condition func() bool, detail string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal(detail)
}

func TestPeriodicRecoveryWaitsForChildAndPersistsQuietHandoffAcrossLostACK(t *testing.T) {
	dir := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	parent := exec.Command(exe, "-test.run=^TestRecoveryGroupHelper$")
	parent.Env = append(os.Environ(), "COORDICA_RECOVERY_HELPER_ROLE=parent", "COORDICA_RECOVERY_HELPER_DIR="+dir)
	parent.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := parent.Start(); err != nil {
		t.Fatal(err)
	}
	parentWaited := false
	t.Cleanup(func() {
		_ = syscall.Kill(-parent.Process.Pid, syscall.SIGKILL)
		if !parentWaited {
			_ = parent.Wait()
		}
	})
	waitRecoveryObservation(t, func() bool { _, err := os.Stat(filepath.Join(dir, "child-ready")); return err == nil }, "isolated child did not start")

	var mu sync.Mutex
	var bodies, keys []string
	var r *Runner
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != "POST" || !strings.HasSuffix(req.URL.Path, "/complete") {
			t.Errorf("unexpected network recovery request: %s %s", req.Method, req.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		raw, _ := io.ReadAll(req.Body)
		var completion domain.CompleteRequest
		if err := json.Unmarshal(raw, &completion); err != nil {
			t.Error(err)
		}
		var durable spool
		if err := readJSON(r.spoolPath("run_test"), &durable); err != nil || durable.Key != req.Header.Get("Idempotency-Key") || durable.Acknowledged || durable.Request == nil || !durable.Request.Quiescent {
			t.Errorf("quiet completion was not persisted before delivery: %v", err)
		}
		if !completion.Quiescent || completion.Status != "stopped" || completion.Generation != 7 || completion.ElapsedSeconds != 12 || completion.SessionID != "prior-session" || completion.Head != "" || completion.ArtifactID != "" {
			t.Errorf("unexpected recovery completion: %+v", completion)
		}
		mu.Lock()
		bodies = append(bodies, string(raw))
		keys = append(keys, req.Header.Get("Idempotency-Key"))
		first := len(bodies) == 1
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
		respond(w, domain.Run{ID: "run_test", Generation: 7, State: "stopped", Quiescent: true})
	}))
	defer server.Close()
	r = testRunner(t, server)
	var events []Event
	r.OnEvent = func(event Event) { events = append(events, event) }
	record := uncertainRecord()
	record.PID = parent.Process.Pid
	record.WorkDir = filepath.Join(r.config.DataDir, "runs", "preserved-workspace")
	if err := os.MkdirAll(record.WorkDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(record.WorkDir, "local-code.txt"), []byte("preserve local work"), 0600); err != nil {
		t.Fatal(err)
	}
	record.Request.ElapsedSeconds = 12
	record.Request.SessionID = "prior-session"
	record.Request.Detail = "test-node-token private prior detail"
	if err := writePrivateJSON(r.spoolPath("run_test"), record); err != nil {
		t.Fatal(err)
	}
	r.flush(context.Background())
	if count, err := r.occupiedSlots(); err != nil || count != 1 {
		t.Fatalf("live parent did not retain local slot: %d %v", count, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "parent-release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := parent.Wait(); err != nil {
		t.Fatal(err)
	}
	parentWaited = true
	if quiet, err := process.TreeQuiescent(record.PID); err != nil || quiet {
		t.Fatalf("parent exit hid its live ordinary child: quiet=%t err=%v", quiet, err)
	}
	r.flush(context.Background())
	mu.Lock()
	count := len(bodies)
	mu.Unlock()
	if count != 0 {
		t.Fatal("uncertain live group caused a network completion")
	}
	var blocked spool
	if err := readJSON(r.spoolPath("run_test"), &blocked); err != nil || blocked.Key != record.Key || !blocked.Acknowledged || blocked.Request.Quiescent {
		t.Fatal("live child changed the fixed uncertain handoff")
	}
	if err := os.WriteFile(filepath.Join(dir, "child-release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	waitRecoveryObservation(t, func() bool { quiet, err := process.TreeQuiescent(record.PID); return err == nil && quiet }, "isolated child did not stop")
	r.flush(context.Background())
	var pending spool
	if err := readJSON(r.spoolPath("run_test"), &pending); err != nil || pending.Acknowledged || pending.Request == nil || !pending.Request.Quiescent || pending.Key == record.Key {
		t.Fatal("lost ACK did not retain the new, durable quiet handoff")
	}
	if count, err := r.occupiedSlots(); err != nil || count != 0 {
		t.Fatalf("stopped tree retained local capacity: %d %v", count, err)
	}
	restarted, err := New(r.Config())
	if err != nil {
		t.Fatal(err)
	}
	restarted.OnEvent = func(event Event) { events = append(events, event) }
	if err := restarted.recoverRecords(context.Background()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if len(bodies) != 2 || bodies[0] != bodies[1] || keys[0] != keys[1] || keys[0] == record.Key {
		t.Errorf("quiet handoff changed across restart and lost ACK: bodies=%v keys=%v", bodies, keys)
	}
	mu.Unlock()
	if _, err := os.Stat(r.spoolPath("run_test")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("confirmed, acknowledged handoff still blocked recovery")
	}
	if content, err := os.ReadFile(filepath.Join(record.WorkDir, "local-code.txt")); err != nil || string(content) != "preserve local work" {
		t.Fatal("recovery discarded local work")
	}
	confirmed := false
	for _, event := range events {
		confirmed = confirmed || event.Kind == "recovery.process_stopped"
		if strings.Contains(event.Detail, "test-node-token") || strings.Contains(event.Detail, record.WorkDir) || strings.Contains(event.Detail, "private prior detail") {
			t.Fatal("recovery diagnostic leaked private state")
		}
	}
	if !confirmed {
		t.Fatal("missing local stop-confirmation diagnostic")
	}
}
