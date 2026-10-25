package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDiscoverySlowVersionsDoNotStarveLaterTools(t *testing.T) {
	executable := fixtureRequest(t, "codex", "unused").Executable
	definitions := []Definition{}
	for _, id := range []string{"slow-first", "slow-second", "healthy-last"} {
		def := Definition{ID: id, Executables: []string{executable}, Protocol: ProtocolCodex}
		if strings.HasPrefix(id, "slow-") {
			def.VersionArgs = []string{"--slow-version"}
		}
		definitions = append(definitions, def)
	}
	registry, err := NewRegistry(definitions...)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	caps := registry.Discover(ctx, nil)
	if len(caps) != 3 || caps[0].Tool != "slow-first" || caps[1].Tool != "slow-second" ||
		caps[2].Tool != "healthy-last" || !caps[2].Available || caps[2].ModelDiscovery != "reported" {
		t.Fatalf("slow version probes hid a healthy later tool or changed catalog ordering: %+v", caps)
	}
	for _, cap := range caps[:2] {
		if cap.Available || !strings.Contains(cap.Detail, "incomplete") {
			t.Fatalf("interrupted discovery looked complete: %+v", cap)
		}
	}
}

func TestModelDiscoverySlowToolDoesNotBlockHealthyToolOrCanceledWaiter(t *testing.T) {
	t.Setenv("AGENT_TEST_CATALOG", "timeout")
	count := filepath.Join(t.TempDir(), "started")
	t.Setenv("AGENT_TEST_CATALOG_COUNT", count)
	executable := fixtureRequest(t, "codex", "unused").Executable
	registry := DefaultRegistry()
	codex, _ := registry.Resolve("codex")
	claude, _ := registry.Resolve("claude")
	slowCtx, stopSlow := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		registry.discoverModels(slowCtx, codex, Capability{Executable: executable, Version: "fixture"})
	}()
	t.Cleanup(func() { stopSlow(); <-done })
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(count); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("slow catalog fixture did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	healthyCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	models, status := registry.discoverModels(healthyCtx, claude, Capability{Executable: executable, Version: "fixture"})
	if status != "reported" || len(models) == 0 || healthyCtx.Err() != nil {
		t.Fatalf("unrelated slow catalog blocked a healthy tool: %s %+v", status, models)
	}
	waiterCtx, cancelWaiter := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancelWaiter()
	start := time.Now()
	models, status = registry.discoverModels(waiterCtx, codex, Capability{Executable: executable, Version: "fixture"})
	if status != "unavailable" || len(models) != 0 || time.Since(start) > time.Second {
		t.Fatal("a canceled waiter blocked on another request's catalog")
	}
	data, err := os.ReadFile(count)
	if err != nil || strings.Count(string(data), "start") != 1 {
		t.Fatal("concurrent reads opened duplicate metadata sessions", err)
	}
}

func TestConcurrentModelDiscoveryReusesOneProbe(t *testing.T) {
	count := filepath.Join(t.TempDir(), "started")
	t.Setenv("AGENT_TEST_CATALOG_COUNT", count)
	executable := fixtureRequest(t, "codex", "unused").Executable
	registry := DefaultRegistry()
	definition, _ := registry.Resolve("codex")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var readers sync.WaitGroup
	for range 8 {
		readers.Go(func() {
			models, status := registry.discoverModels(ctx, definition, Capability{Executable: executable, Version: "fixture"})
			if status != "reported" || len(models) != 1 {
				t.Errorf("shared probe lost its result: %s %+v", status, models)
			}
		})
	}
	readers.Wait()
	data, err := os.ReadFile(count)
	if err != nil || strings.Count(string(data), "start") != 1 {
		t.Fatal("same catalog was opened multiple times", err)
	}
}
