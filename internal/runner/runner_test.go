package runner

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"coordica/internal/agent"
	"coordica/internal/domain"
	"coordica/internal/gitops"
)

func testRunner(t *testing.T, server *httptest.Server) *Runner {
	t.Helper()
	overrides := map[string]string{}
	for _, definition := range agent.BuiltinDefinitions() {
		overrides[definition.ID] = filepath.Join(t.TempDir(), "not-installed", definition.ID)
	}
	r, err := New(Config{CenterURL: server.URL, DataDir: t.TempDir(), NodeID: "node_test", Token: "test-node-token", Executables: overrides})
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func respond(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(value)
}
func testRecord() spool {
	return spool{Assignment: domain.Assignment{Run: domain.Run{ID: "run_test", NodeID: "node_test", Generation: 7}}, Key: "stable-completion-key", Request: &domain.CompleteRequest{Generation: 7, Status: "succeeded", Quiescent: true, Head: strings.Repeat("a", 40), ArtifactID: "artifact_test"}}
}

func TestHTTPRequiresTLSOutsideLoopback(t *testing.T) {
	_, err := New(Config{CenterURL: "http://192.0.2.1:8080", DataDir: t.TempDir()})
	if err == nil {
		t.Fatal("LAN plaintext accepted")
	}
	_, err = New(Config{CenterURL: "https://example.invalid", DataDir: t.TempDir(), TLSCA: filepath.Join(t.TempDir(), "missing")})
	if err == nil {
		t.Fatal("missing CA silently ignored")
	}
}
func TestInputCannotSendNodeTokenToAnotherOrigin(t *testing.T) {
	called := false
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer other.Close()
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	r := testRunner(t, server)
	_, err := r.request(context.Background(), "GET", other.URL, "", nil, "", io.Discard)
	if err == nil || called {
		t.Fatal("foreign input origin accepted")
	}
}
func TestPairingPersistsPrivateIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/api/v1/runner/register" {
			t.Error(req.URL.Path)
		}
		var input domain.RegisterRequest
		json.NewDecoder(req.Body).Decode(&input)
		if input.Code != "once" || input.Capacity != 2 {
			t.Error("invalid registration")
		}
		respond(w, domain.RegisterResponse{NodeID: "node_paired", Token: "fixture-token"})
	}))
	defer server.Close()
	r := testRunner(t, server)
	r.config.NodeID = ""
	r.config.Token = ""
	if err := r.Register(context.Background(), "once"); err != nil {
		t.Fatal(err)
	}
	config, err := Load(r.config.DataDir)
	if err != nil || config.NodeID != "node_paired" || config.Token != "fixture-token" {
		t.Fatal("identity was not persisted")
	}
	if stat, err := os.Stat(filepath.Join(config.DataDir, "runner.json")); err != nil || (runtime.GOOS != "windows" && stat.Mode().Perm()&0077 != 0) {
		t.Fatalf("private file mode: %v %v", stat, err)
	}
}
func TestLostCompletionACKReplaysIdenticalRequestAndKey(t *testing.T) {
	var mu sync.Mutex
	var bodies, keys []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		raw, _ := io.ReadAll(req.Body)
		mu.Lock()
		bodies = append(bodies, string(raw))
		keys = append(keys, req.Header.Get("Idempotency-Key"))
		first := len(bodies) == 1
		mu.Unlock()
		if req.Header.Get("Authorization") != "Bearer test-node-token" {
			t.Error("missing node identity")
		}
		if first {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			conn.Close()
			return
		}
		respond(w, domain.Run{ID: "run_test", Generation: 7, State: "succeeded", Quiescent: true})
	}))
	defer server.Close()
	r := testRunner(t, server)
	record := testRecord()
	if err := writePrivateJSON(r.spoolPath("run_test"), record); err != nil {
		t.Fatal(err)
	}
	if err := r.deliver(context.Background(), record); err == nil {
		t.Fatal("lost ACK looked successful")
	}
	var restored spool
	if err := readJSON(r.spoolPath("run_test"), &restored); err != nil {
		t.Fatal(err)
	}
	if err := r.deliver(context.Background(), restored); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 2 || bodies[0] != bodies[1] || keys[0] != keys[1] {
		t.Fatal("completion changed on retry")
	}
	if _, err := os.Stat(r.spoolPath("run_test")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("acknowledged completion still pending")
	}
}
func TestRecoveryDoesNotInventQuiescence(t *testing.T) {
	quiet := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method == "POST" {
			var input domain.CompleteRequest
			json.NewDecoder(req.Body).Decode(&input)
			quiet = input.Quiescent
		}
		respond(w, domain.Run{ID: "run_test", Generation: 7, State: "lost"})
	}))
	defer server.Close()
	r := testRunner(t, server)
	record := testRecord()
	record.Request = nil
	record.PID = 0
	if err := writePrivateJSON(r.spoolPath("run_test"), record); err != nil {
		t.Fatal(err)
	}
	if err := r.recoverRecords(context.Background()); err != nil {
		t.Fatal(err)
	}
	if quiet {
		t.Fatal("missing process identity reported stopped")
	}
	var kept spool
	if err := readJSON(r.spoolPath("run_test"), &kept); err != nil || !kept.Acknowledged {
		t.Fatal("unknown process record discarded")
	}
	if count, err := r.occupiedSlots(); err != nil || count != 1 {
		t.Fatalf("unknown process freed capacity: %d %v", count, err)
	}
}

func TestLostClaimACKPreservesRequestAcrossRestart(t *testing.T) {
	var bodies, keys []string
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		raw, _ := io.ReadAll(req.Body)
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
			conn.Close()
			return
		}
		respond(w, domain.Assignment{Run: domain.Run{ID: "run_test", NodeID: "node_test", Generation: 7}})
	}))
	defer server.Close()
	r := testRunner(t, server)
	if _, _, err := r.claim(context.Background()); err == nil {
		t.Fatal("lost claim ACK looked successful")
	}
	restarted, err := New(r.Config())
	if err != nil {
		t.Fatal(err)
	}
	a, _, err := restarted.claim(context.Background())
	if err != nil || a == nil || a.Run.ID != "run_test" {
		t.Fatalf("a=%+v err=%v", a, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 2 || bodies[0] != bodies[1] || keys[0] != keys[1] {
		t.Fatal("claim changed after Runner restart")
	}
}

func TestReadOnlyReviewReceivesCompleteFixedDiffOrBlocks(t *testing.T) {
	ctx := context.Background()
	git := gitops.New("git")
	repo := filepath.Join(t.TempDir(), "review source")
	if err := git.Init(ctx, repo, false); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(repo, "代码.txt")
	if err := os.WriteFile(path, []byte("old behavior\n"), 0600); err != nil {
		t.Fatal(err)
	}
	base, err := git.CommitAll(ctx, repo, "base", gitops.Identity{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("new behavior\n"), 0600); err != nil {
		t.Fatal(err)
	}
	head, err := git.CommitAll(ctx, repo, "change", gitops.Identity{})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	r := testRunner(t, server)
	a := domain.Assignment{Run: domain.Run{Kind: "review", Base: head.String()}, Candidate: &domain.Candidate{Main: base.String(), Commit: head.String()}}
	input, err := r.reviewInput(ctx, a, repo)
	if err != nil || !strings.Contains(input, "-old behavior") || !strings.Contains(input, "+new behavior") || !strings.Contains(input, "代码.txt") {
		t.Fatalf("review input missing full change: %s %v", input, err)
	}
	if err := os.WriteFile(path, []byte(strings.Repeat("large review line\n", maxReviewInput/16)), 0600); err != nil {
		t.Fatal(err)
	}
	large, err := git.CommitAll(ctx, repo, "large change", gitops.Identity{})
	if err != nil {
		t.Fatal(err)
	}
	a.Run.Base = large.String()
	a.Candidate.Commit = large.String()
	input, err = r.reviewInput(ctx, a, repo)
	if err == nil || input != "" {
		t.Fatal("large review silently truncated or accepted")
	}
	if head, err := git.Head(ctx, repo); err != nil || head != large {
		t.Fatal("reading review changed candidate")
	}
}

func TestRunnerSurfacesFailureAndUnavailableCapabilities(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		respond(w, domain.Problem{Code: "revoked", Message: "node revoked"})
	}))
	defer server.Close()
	r := testRunner(t, server)
	var events []Event
	r.OnEvent = func(e Event) { events = append(events, e) }
	if err := r.Run(context.Background()); err == nil {
		t.Fatal("revoked node continued")
	}
	capabilities, failed := 0, false
	for _, event := range events {
		if event.Kind == "capability" && strings.Contains(event.Detail, "available=false") {
			capabilities++
		}
		if event.Kind == "claim.failed" && strings.Contains(event.Detail, "401") {
			failed = true
		}
	}
	if capabilities != len(r.registry.Definitions()) || !failed {
		t.Fatalf("invisible errors: %+v", events)
	}
}
func TestPermissionDeadlinesUseCenterTime(t *testing.T) {
	local := time.Now()
	server := local.Add(12 * time.Hour)
	deadline, err := localDeadline(server.Add(10*time.Second), responseClock{Server: server, Sent: local, Received: local.Add(100 * time.Millisecond)})
	if err != nil || deadline.Sub(local) > 9*time.Second || deadline.Sub(local) < 8*time.Second {
		t.Fatalf("deadline=%v err=%v", deadline, err)
	}
	if _, err = localDeadline(server, responseClock{}); err == nil {
		t.Fatal("missing center clock accepted")
	}
}
func TestWatchdogStopsBeforeLeaseAndRevocation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		respond(w, domain.HeartbeatResponse{Cancel: true, Reason: "paused"})
	}))
	defer server.Close()
	r := testRunner(t, server)
	for _, leaseDuration := range []time.Duration{150 * time.Millisecond, 5 * time.Second} {
		ctx, stop := context.WithCancel(context.Background())
		now := time.Now()
		a := domain.Assignment{Run: domain.Run{ID: "run_test", Generation: 7}, HeartbeatSeconds: 1}
		end := r.watchPermission(ctx, stop, a, now, now.Add(leaseDuration), now.Add(time.Minute), now)
		select {
		case <-ctx.Done():
		case <-time.After(2 * time.Second):
			t.Fatal("local permission watchdog did not stop")
		}
		end()
		stop()
	}
}
func TestStrictReviewRejectsWrongCandidateAndTrailingText(t *testing.T) {
	valid := `{"candidate":"candidate","verdict":"approve","summary":"checked","findings":[],"run_id":""}`
	if _, err := parseReview(valid, "candidate"); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{valid + " trailing", valid + ` {}`, strings.Replace(valid, `"approve"`, `"unknown"`, 1), strings.Replace(valid, `"candidate":"candidate"`, `"candidate":"other"`, 1), "```json\n" + valid + "\n```"} {
		if _, err := parseReview(text, "candidate"); err == nil {
			t.Fatal("invalid review accepted")
		}
	}
}

func TestWorkerRequiresAgentCommitAndPreservesDirtyWorkspace(t *testing.T) {
	ctx := context.Background()
	git := gitops.New("git")
	source := filepath.Join(t.TempDir(), "source")
	if err := git.Init(ctx, source, false); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "code.txt"), []byte("base"), 0600); err != nil {
		t.Fatal(err)
	}
	base, err := git.CommitAll(ctx, source, "base", gitops.Identity{Name: "Test", Email: "test@example.invalid"})
	if err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(t.TempDir(), "input.bundle")
	if _, err := git.CreateSnapshotBundle(ctx, source, base, bundle); err != nil {
		t.Fatal(err)
	}
	var completion domain.CompleteRequest
	var completeMu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case strings.HasSuffix(req.URL.Path, "input.bundle"):
			http.ServeFile(w, req, bundle)
		case strings.HasSuffix(req.URL.Path, "complete"):
			var input domain.CompleteRequest
			json.NewDecoder(req.Body).Decode(&input)
			completeMu.Lock()
			completion = input
			completeMu.Unlock()
			respond(w, domain.Run{ID: "run_test", Generation: 7, Quiescent: true, State: "failed"})
		default:
			t.Error("unexpected worker request", req.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	r := testRunner(t, server)
	if err := r.registry.Register(agent.Definition{ID: "agent-commit-fixture", Executables: []string{"fixture"}, Protocol: agent.ProtocolCodex, CommitMode: "agent"}); err != nil {
		t.Fatal(err)
	}
	r.execute = func(ctx context.Context, req agent.Request, onEvent func(agent.Event)) (agent.Result, error) {
		if err := os.WriteFile(filepath.Join(req.WorkDir, "code.txt"), []byte("uncommitted"), 0600); err != nil {
			return agent.Result{}, err
		}
		return agent.Result{ExitCode: 0, TreeStopped: true, OutputText: "need scope test-node-token", SessionID: "fixture-session"}, nil
	}
	now := time.Now()
	a := domain.Assignment{Run: domain.Run{ID: "run_test", NodeID: "node_test", Generation: 7, Kind: "implement", Tool: "agent-commit-fixture", Base: base.String(), LeaseUntil: now.Add(time.Minute), Deadline: now.Add(time.Minute)}, Task: domain.Task{Title: "change", Description: "change code", Acceptance: "test", AllowedPaths: []string{"code.txt"}}, InputURL: "/api/v1/runs/run_test/input.bundle"}
	record := spool{Assignment: a, Key: "worker-key", StartedAt: now, WorkDir: filepath.Join(r.config.DataDir, "runs", "空间 with spaces", "工作区")}
	if err := writePrivateJSON(r.spoolPath(a.Run.ID), record); err != nil {
		t.Fatal(err)
	}
	r.perform(ctx, record, responseClock{Server: now, Sent: now, Received: now})
	completeMu.Lock()
	defer completeMu.Unlock()
	if completion.Status != "failed" || completion.ArtifactID != "" {
		t.Fatalf("unexpected completion: %+v", completion)
	}
	if !strings.Contains(completion.Detail, "need scope [redacted]") || strings.Contains(completion.Detail, "test-node-token") {
		t.Fatal("model blocker or secret handling missing from failure detail")
	}
	if log, err := os.ReadFile(filepath.Join(filepath.Dir(record.WorkDir), "events.jsonl")); err != nil || !strings.Contains(string(log), "runner.result") || strings.Contains(string(log), "test-node-token") {
		t.Fatalf("local evidence missing or unredacted: %v", err)
	}
	head, err := git.Head(ctx, record.WorkDir)
	if err != nil || head != base {
		t.Fatalf("Runner staged or committed unexpected changes: %s %v", head, err)
	}
	if contents, err := os.ReadFile(filepath.Join(record.WorkDir, "code.txt")); err != nil || string(contents) != "uncommitted" {
		t.Fatal("private changes were discarded")
	}
}
