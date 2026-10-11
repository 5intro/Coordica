package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"coordica/internal/agent"
	"coordica/internal/filelock"
	"coordica/internal/runner"
)

func consoleRunnerConfig(t *testing.T, center, console string) string {
	t.Helper()
	dir := t.TempDir()
	executables := map[string]string{}
	for _, definition := range agent.BuiltinDefinitions() {
		// Version discovery cannot launch installed tools or make model calls.
		executables[definition.ID] = filepath.Join(dir, "uninstalled", definition.ID)
	}
	config := runner.Config{CenterURL: center, DataDir: dir, NodeID: "own-node", Token: "private-runner-token", Capacity: 1, PollInterval: 20 * time.Millisecond, ConsoleListen: console, Executables: executables}
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "runner.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func idleRunnerCenter(t *testing.T) (*httptest.Server, <-chan struct{}) {
	t.Helper()
	claims := make(chan struct{}, 32)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer private-runner-token" {
			t.Errorf("unexpected center credential: %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "POST /api/v1/runner/claim":
			_, _ = io.WriteString(w, "null")
			select {
			case claims <- struct{}{}:
			default:
			}
		case "GET /api/v1/runner/state":
			_, _ = io.WriteString(w, `{"api_version":"v1","node":{"id":"own-node","name":"this machine"},"tasks":[],"runs":[],"requirements":[],"token":"private-runner-token"}`)
		default:
			t.Errorf("unexpected Runner request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server, claims
}

func awaitRunnerClaim(t *testing.T, claims <-chan struct{}, done <-chan error) {
	t.Helper()
	select {
	case <-claims:
	case err := <-done:
		t.Fatalf("Runner stopped before claiming: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("Runner did not claim from center")
	}
}

func stopConsoleRunner(t *testing.T, cancel context.CancelFunc, done <-chan error) {
	t.Helper()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Error("Runner shutdown", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("Runner did not stop after cancellation")
	}
}

func TestRunnerBusyConsolePortStillClaimsAndStops(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	center, claims := idleRunnerCenter(t)
	dir := consoleRunnerConfig(t, center.URL, occupied.Addr().String())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runRunner(ctx, []string{"--data", dir}) }()
	awaitRunnerClaim(t, claims, done)
	awaitRunnerClaim(t, claims, done)
	stopConsoleRunner(t, cancel, done)
	unlock, err := filelock.Acquire(filepath.Join(dir, "runner.lock"))
	if err != nil {
		t.Fatal("Runner shutdown retained the data-directory lock", err)
	}
	unlock()
}

func TestRunnerLocalConsoleIsIndependentOfBrowserAndClosesOnShutdown(t *testing.T) {
	center, claims := idleRunnerCenter(t)
	address := freeAddress(t)
	dir := consoleRunnerConfig(t, center.URL, address)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runRunner(ctx, []string{"--data", dir}) }()
	awaitRunnerClaim(t, claims, done)
	viewer := &http.Client{Timeout: time.Second}
	response, err := viewer.Get("http://" + address + "/api/v1/local/state")
	if err != nil {
		stopConsoleRunner(t, cancel, done)
		t.Fatal("local console unavailable", err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	viewer.CloseIdleConnections()
	if err != nil || response.StatusCode != http.StatusOK || !strings.Contains(string(body), `"id":"own-node"`) || strings.Contains(string(body), "private-runner-token") {
		stopConsoleRunner(t, cancel, done)
		t.Fatalf("local state %d: %s (%v)", response.StatusCode, body, err)
	}
	// No viewer remains connected. Polling must continue on its own.
	for len(claims) > 0 {
		<-claims
	}
	awaitRunnerClaim(t, claims, done)
	stopConsoleRunner(t, cancel, done)
	probe, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatal("Runner console retained its listener after shutdown", err)
	}
	probe.Close()
}

func TestRunnerConsoleRejectsNonLoopbackBinding(t *testing.T) {
	dir := consoleRunnerConfig(t, "http://127.0.0.1:1", "0.0.0.0:8081")
	err := runRunner(context.Background(), []string{"--data", dir})
	if err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatal("non-loopback local console accepted", err)
	}
}
