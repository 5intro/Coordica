package httpapi

import (
	"bytes"
	"context"
	"coordica/internal/app"
	"coordica/internal/domain"
	"coordica/internal/store"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestAuthenticationStrictInputAndNodeBoundary(t *testing.T) {
	dir := t.TempDir()
	db, e := store.Open(filepath.Join(dir, "coordica.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	s := app.New(db, dir)
	path, e := s.Bootstrap(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	token, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	handler := (&Server{Service: s}).Handler()
	request := func(method, path, auth, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		r.Host = "127.0.0.1:8080"
		r.RemoteAddr = "127.0.0.1:12345"
		r.Header.Set("Authorization", "Bearer "+auth)
		r.Header.Set("Idempotency-Key", app.ID("test"))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if w := request("GET", "/api/v1/state", "invalid", ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := request("POST", "/api/v1/project", strings.TrimSpace(string(token)), `{"unknown":1}`); w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request("POST", "/api/v1/project", strings.TrimSpace(string(token)), `{} {}`); w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
	w := request("GET", "/api/v1/state", strings.TrimSpace(string(token)), "")
	if w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	var doc map[string]any
	if json.Unmarshal(w.Body.Bytes(), &doc) != nil || doc["api_version"] != "v1" {
		t.Fatal(w.Body.String())
	}
	if strings.Contains(w.Body.String(), "own_") {
		t.Fatal("credential leaked")
	}
}

type boundaryFixture struct {
	server *Server
	owner  string
	node   domain.RegisterResponse
}

func newBoundaryFixture(t *testing.T) *boundaryFixture {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "coordica.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	service := app.New(db, dir)
	path, err := service.Bootstrap(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	token, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	f := &boundaryFixture{server: &Server{Service: service, Integration: &app.Integrator{Service: service}, UI: fstest.MapFS{"index.html": {Data: []byte("Coordica console")}}}, owner: strings.TrimSpace(string(token))}
	f.node = f.register(t, "First node")
	return f
}

func boundaryRequest(handler http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Host = "127.0.0.1:8080"
	r.RemoteAddr = "127.0.0.1:12345"
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	r.Header.Set("Idempotency-Key", app.ID("request_"))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func (f *boundaryFixture) register(t *testing.T, name string) domain.RegisterResponse {
	t.Helper()
	w := boundaryRequest(f.server.AdminHandler(), "POST", "/api/v1/pairings", f.owner, fmt.Sprintf(`{"name":%q}`, name))
	if w.Code != http.StatusOK {
		t.Fatal(w.Code, w.Body.String())
	}
	var pairing struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &pairing); err != nil {
		t.Fatal(err)
	}
	w = boundaryRequest(f.server.RunnerHandler(), "POST", "/api/v1/runner/register", "", fmt.Sprintf(`{"code":%q,"name":%q,"platform":"test","capacity":1}`, pairing.Code, name))
	if w.Code != http.StatusOK {
		t.Fatal(w.Code, w.Body.String())
	}
	var registration domain.RegisterResponse
	if err := json.Unmarshal(w.Body.Bytes(), &registration); err != nil {
		t.Fatal(err)
	}
	return registration
}

func (f *boundaryFixture) runningAssignment(t *testing.T) domain.Assignment {
	t.Helper()
	ctx := context.Background()
	owner, err := f.server.Service.Authenticate(ctx, f.owner)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.server.Service.CreateProject(ctx, owner, "project", domain.Project{Name: "Test", RepositoryURL: f.server.Service.Dir, TargetBranch: "main", TotalBudgetSeconds: 900, Checks: []domain.Check{{Name: "check", Command: "git", Args: []string{"status"}, TimeoutSeconds: 10}}})
	if err != nil {
		t.Fatal(err)
	}
	body, err := f.server.Service.CreateTask(ctx, owner, "task", domain.Task{Title: "Task", Description: "Change source", Acceptance: "Test", AllowedPaths: []string{"src/**"}, Tool: "codex", BudgetSeconds: 300, MaxRunSeconds: 60, MaxRetries: 1, MaxRepairs: 1})
	if err != nil {
		t.Fatal(err)
	}
	var task domain.Task
	if err = json.Unmarshal(body, &task); err != nil {
		t.Fatal(err)
	}
	if _, err = f.server.Service.TaskAction(ctx, owner, "approve", task.ID, "approve", domain.VersionRequest{ExpectedVersion: task.Version}); err != nil {
		t.Fatal(err)
	}
	node, err := f.server.Service.Authenticate(ctx, f.node.Token)
	if err != nil {
		t.Fatal(err)
	}
	body, err = f.server.Service.Claim(ctx, node, "claim", domain.ClaimRequest{Capabilities: []domain.Capability{{Tool: "codex", Available: true, Structured: true, Cancel: true, Review: true}}}, strings.Repeat("a", 40))
	if err != nil {
		t.Fatal(err)
	}
	var assignment domain.Assignment
	if err = json.Unmarshal(body, &assignment); err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(f.server.Service.Dir, "inputs", assignment.Run.ID+".bundle")
	if err = os.MkdirAll(filepath.Dir(input), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(input, []byte("fixed input bundle"), 0600); err != nil {
		t.Fatal(err)
	}
	return assignment
}

func TestRunnerSurfaceHasNoConsoleOrOwnerAPI(t *testing.T) {
	f := newBoundaryFixture(t)
	handler := f.server.RunnerHandler()
	for _, route := range []struct{ method, path string }{
		{"GET", "/"}, {"GET", "/index.html"}, {"GET", "/assets/app.js"},
		{"GET", "/api/v1/local/info"},
		{"GET", "/api/v1/state"}, {"GET", "/api/v1/export"},
		{"GET", "/api/v1/system/connection"},
		{"POST", "/api/v1/pairings"}, {"POST", "/api/v1/project"},
		{"POST", "/api/v1/project/pause"}, {"POST", "/api/v1/requirements"},
		{"POST", "/api/v1/tasks"}, {"POST", "/api/v1/tasks/task_test/approve"},
		{"POST", "/api/v1/nodes/node_test/revoke"}, {"POST", "/api/v1/backups"},
		{"POST", "/api/v1/runs/run_test/confirm-stopped"},
		{"GET", "/api/v1/runner/register"}, {"PUT", "/api/v1/runs/run_test/heartbeat"},
		{"POST", "/api/v1/runs/run_test/unknown"},
	} {
		for _, token := range []string{"", f.owner, f.node.Token} {
			w := boundaryRequest(handler, route.method, route.path, token, `{}`)
			if w.Code != http.StatusNotFound {
				t.Errorf("%s %s: got %d, want 404", route.method, route.path, w.Code)
			}
		}
	}
	w := boundaryRequest(handler, "GET", "/healthz", "", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"api_version":"v1"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	owner, err := f.server.Service.Authenticate(context.Background(), f.owner)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := f.server.Service.Snapshot(context.Background(), owner)
	if err != nil || snapshot.Project != nil || len(snapshot.Tasks) != 0 || len(snapshot.Nodes) != 1 {
		t.Fatal("denied network requests changed state", err)
	}
}

func TestRuntimeConnectionConfigurationStaysOwnerLocal(t *testing.T) {
	f := newBoundaryFixture(t)
	f.server.ConnectionInfo = domain.ConnectionInfo{
		APIVersion: "v1", Version: "test-version", ConsoleURL: "http://127.0.0.1:8080",
		RunnerURL: "https://192.0.2.1:8443", RunnerConsoleURL: "http://127.0.0.1:8081",
		RunnerListen: "0.0.0.0:8443", ConsoleListen: "127.0.0.1:8080", DataDir: "private-runtime-directory",
		CertificatePEM: "public-certificate", CertificateSHA256: "public-fingerprint",
	}
	for _, handler := range []http.Handler{f.server.AdminHandler(), f.server.Handler()} {
		w := boundaryRequest(handler, "GET", "/api/v1/system/connection", f.owner, "")
		var config domain.ConnectionInfo
		if err := json.Unmarshal(w.Body.Bytes(), &config); err != nil || w.Code != http.StatusOK || config != f.server.ConnectionInfo {
			t.Fatal("runtime configuration changed in transport", w.Code, err, w.Body.String())
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("runtime configuration can be cached")
		}
		for _, token := range []string{"", f.node.Token} {
			w = boundaryRequest(handler, "GET", "/api/v1/system/connection", token, "")
			if w.Code < 400 || strings.Contains(w.Body.String(), f.server.ConnectionInfo.DataDir) {
				t.Fatal("non-owner read center configuration", w.Code, w.Body.String())
			}
		}
	}
	for _, token := range []string{"", f.owner, f.node.Token} {
		w := boundaryRequest(f.server.RunnerHandler(), "GET", "/api/v1/system/connection", token, "")
		if w.Code != http.StatusNotFound || strings.Contains(w.Body.String(), f.server.ConnectionInfo.DataDir) {
			t.Fatal("network surface exposed owner runtime information", w.Code, w.Body.String())
		}
	}
	w := boundaryRequest(f.server.RunnerHandler(), "GET", "/api/v1/runner/local-entry", f.node.Token, "")
	var entry domain.CenterEntry
	if err := json.Unmarshal(w.Body.Bytes(), &entry); err != nil || w.Code != http.StatusOK || entry.ConsoleURL != f.server.ConnectionInfo.ConsoleURL || entry.RunnerURL != f.server.ConnectionInfo.RunnerURL {
		t.Fatal("node entry does not use configured runtime addresses", w.Code, err, w.Body.String())
	}
	for _, private := range []string{f.server.ConnectionInfo.DataDir, f.server.ConnectionInfo.CertificatePEM, f.server.ConnectionInfo.CertificateSHA256, f.owner, f.node.Token} {
		if strings.Contains(w.Body.String(), private) {
			t.Fatal("node entry leaked owner runtime configuration", w.Body.String())
		}
	}
	if w = boundaryRequest(f.server.RunnerHandler(), "GET", "/api/v1/runner/local-entry", f.owner, ""); w.Code != http.StatusForbidden {
		t.Fatal("owner token accepted on node entry", w.Code)
	}
	if w = boundaryRequest(f.server.AdminHandler(), "GET", "/api/v1/runner/local-entry", f.owner, ""); w.Code != http.StatusNotFound {
		t.Fatal("admin surface exposed node entry", w.Code)
	}
}

func TestConsoleRequiresLocalPeerHostAndOwner(t *testing.T) {
	f := newBoundaryFixture(t)
	for _, handler := range []http.Handler{f.server.AdminHandler(), f.server.Handler()} {
		for _, route := range []struct{ method, path string }{{"GET", "/api/v1/state"}, {"POST", "/api/v1/pairings"}, {"POST", "/api/v1/project"}} {
			w := boundaryRequest(handler, route.method, route.path, f.node.Token, `{}`)
			if w.Code != http.StatusForbidden {
				t.Fatal(route, w.Code, w.Body.String())
			}
		}
		for _, peerHost := range []struct{ peer, host string }{
			{"192.0.2.1:12345", "localhost:8080"},
			{"127.0.0.1:12345", "attacker.example:8080"},
			{"127.0.0.1:12345", "192.0.2.1:8080"},
			{"invalid", "localhost:8080"},
		} {
			for _, path := range []string{"/", "/healthz", "/api/v1/state"} {
				r := httptest.NewRequest("GET", path, nil)
				r.RemoteAddr, r.Host = peerHost.peer, peerHost.host
				r.Header.Set("Authorization", "Bearer "+f.owner)
				r.Header.Set("X-Forwarded-For", "127.0.0.1")
				r.Header.Set("X-Forwarded-Host", "localhost:8080")
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, r)
				if w.Code != http.StatusForbidden || strings.Contains(w.Body.String(), "Coordica console") {
					t.Fatal(peerHost, path, w.Code, w.Body.String())
				}
			}
		}
		for _, host := range []string{"localhost:8080", "LOCALHOST", "127.0.0.1:8080", "[::1]:8080", "[::1]"} {
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr, r.Host = "[::1]:12345", host
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != http.StatusOK || w.Body.String() != "Coordica console" {
				t.Fatal(host, w.Code, w.Body.String())
			}
		}
		if w := boundaryRequest(handler, "GET", "/api/v1/state", f.owner, ""); w.Code != http.StatusOK {
			t.Fatal(w.Code, w.Body.String())
		}
		if w := boundaryRequest(handler, "GET", "/api/v1/local/info", "", ""); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"mode":"center"`) {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}

func TestRunnerRuntimeRoutesKeepNodeIdentityAndRunOwnership(t *testing.T) {
	f := newBoundaryFixture(t)
	a := f.runningAssignment(t)
	other := f.register(t, "Second node")
	for _, handler := range []http.Handler{f.server.RunnerHandler(), f.server.Handler()} {
		for _, route := range []struct{ method, path string }{
			{"POST", "/api/v1/runner/claim"},
			{"GET", "/api/v1/runner/state"},
			{"POST", "/api/v1/runner/requirements"},
			{"GET", "/api/v1/runs/" + a.Run.ID},
			{"GET", "/api/v1/runs/" + a.Run.ID + "/context"},
			{"GET", "/api/v1/runs/" + a.Run.ID + "/input.bundle"},
			{"POST", "/api/v1/runs/" + a.Run.ID + "/heartbeat"},
			{"POST", "/api/v1/runs/" + a.Run.ID + "/artifacts"},
			{"POST", "/api/v1/runs/" + a.Run.ID + "/block"},
			{"POST", "/api/v1/runs/" + a.Run.ID + "/complete"},
		} {
			if w := boundaryRequest(handler, route.method, route.path, f.owner, `{}`); w.Code != http.StatusForbidden {
				t.Fatal("owner used a node endpoint", route, w.Code, w.Body.String())
			}
			if w := boundaryRequest(handler, route.method, route.path, "", `{}`); w.Code != http.StatusUnauthorized {
				t.Fatal("anonymous used a node endpoint", route, w.Code, w.Body.String())
			}
		}
		for _, suffix := range []string{"", "/context", "/input.bundle"} {
			path := "/api/v1/runs/" + a.Run.ID + suffix
			if w := boundaryRequest(handler, "GET", path, f.node.Token, ""); w.Code != http.StatusOK {
				t.Fatal(path, w.Code, w.Body.String())
			}
			if w := boundaryRequest(handler, "GET", path, other.Token, ""); w.Code != http.StatusForbidden {
				t.Fatal("different node read a run", path, w.Code, w.Body.String())
			}
		}
	}
	if w := boundaryRequest(f.server.AdminHandler(), "GET", "/api/v1/runs/"+a.Run.ID, f.owner, ""); w.Code != http.StatusNotFound {
		t.Fatal("admin surface exposed node execution", w.Code, w.Body.String())
	}
	handler := f.server.RunnerHandler()
	w := boundaryRequest(handler, "POST", "/api/v1/runner/requirements", f.node.Token, `{"title":"New search","body":"Search names"}`)
	if w.Code != http.StatusOK {
		t.Fatal(w.Code, w.Body.String())
	}
	var requirement domain.Requirement
	if err := json.Unmarshal(w.Body.Bytes(), &requirement); err != nil || requirement.SourceNodeID != f.node.NodeID {
		t.Fatal("requirement source was not derived from node identity", err, w.Body.String())
	}
	if w = boundaryRequest(handler, "POST", "/api/v1/runner/requirements", f.node.Token, `{"title":"Forged","body":"Source","source_node_id":"other"}`); w.Code != http.StatusBadRequest {
		t.Fatal("proposal accepted an injected node identity", w.Code, w.Body.String())
	}
	w = boundaryRequest(handler, "GET", "/api/v1/runner/state", f.node.Token, "")
	if w.Code != http.StatusOK {
		t.Fatal(w.Code, w.Body.String())
	}
	var snapshot domain.NodeSnapshot
	if err := json.Unmarshal(w.Body.Bytes(), &snapshot); err != nil || snapshot.Node.ID != f.node.NodeID || len(snapshot.Runs) != 1 || len(snapshot.Requirements) != 1 || strings.Contains(w.Body.String(), other.NodeID) {
		t.Fatal("node snapshot crossed the node boundary", err, w.Body.String())
	}
	for _, route := range []struct{ path, body string }{
		{"/api/v1/runner/claim", `{"capabilities":[]}`},
		{"/api/v1/runs/" + a.Run.ID + "/heartbeat", fmt.Sprintf(`{"generation":%d,"elapsed_seconds":0}`, a.Run.Generation)},
		{"/api/v1/runs/" + a.Run.ID + "/artifacts", "candidate bundle fixture"},
		{"/api/v1/runs/" + a.Run.ID + "/block", fmt.Sprintf(`{"generation":%d,"reason":"needs owner decision"}`, a.Run.Generation)},
		{"/api/v1/runs/" + a.Run.ID + "/complete", fmt.Sprintf(`{"generation":%d,"status":"cancelled","quiescent":true}`, a.Run.Generation)},
	} {
		if w := boundaryRequest(handler, "POST", route.path, f.node.Token, route.body); w.Code != http.StatusOK {
			t.Fatal(route.path, w.Code, w.Body.String())
		}
	}
	owner, err := f.server.Service.Authenticate(context.Background(), f.owner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.server.Service.RevokeNode(context.Background(), owner, "revoke", f.node.NodeID); err != nil {
		t.Fatal(err)
	}
	if w := boundaryRequest(handler, "GET", "/api/v1/runs/"+a.Run.ID, f.node.Token, ""); w.Code != http.StatusUnauthorized {
		t.Fatal("revoked node accepted", w.Code, w.Body.String())
	}
}
