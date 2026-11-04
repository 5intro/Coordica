package runtime

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"coordica/internal/process"
)

func TestMain(m *testing.M) {
	if os.Getenv("COORDICA_BINDING_FIXTURE") == "1" {
		if len(os.Args) != 2 || os.Args[1] != "runtime-binding-fixture" {
			os.Exit(2)
		}
		reader := bufio.NewScanner(os.Stdin)
		reader.Buffer(make([]byte, 1024), 32<<10)
		if !reader.Scan() {
			os.Exit(3)
		}
		var call NativeCreate
		if json.Unmarshal(reader.Bytes(), &call) != nil {
			os.Exit(4)
		}
		if err := os.WriteFile(os.Getenv("COORDICA_BINDING_JOURNAL"), []byte(call.CommandID+"\n"), 0600); err != nil {
			os.Exit(5)
		}
		result := NativeCreation{Outcome: "confirmed", ID: NativeSessionID{Harness: call.Config.Target.Harness, Value: "fixture-" + call.BindingID}, Effective: call.Config}
		raw, _ := json.Marshal(result)
		fmt.Println(string(raw))
		for reader.Scan() {
			os.Exit(6)
		} // No prompt, turn or follow-up command is accepted.
		os.Exit(0)
	}
	os.Exit(m.Run())
}

type processBindingPort struct {
	mu        sync.Mutex
	life      context.Context
	directory string
	journal   string
	child     *process.Session
	id        NativeSessionID
	call      NativeCreate
	before    func(context.Context) error
	creates   atomic.Int32
	stops     atomic.Int32
}

func newProcessBindingPort(t *testing.T) *processBindingPort {
	t.Helper()
	dir := t.TempDir()
	p := &processBindingPort{life: t.Context(), directory: dir, journal: filepath.Join(dir, "created.log")}
	t.Cleanup(func() {
		p.mu.Lock()
		child := p.child
		p.mu.Unlock()
		if child != nil {
			child.Stop()
			for range child.Output() {
			}
			if result, err := child.Wait(); !result.TreeStopped {
				t.Errorf("fixture tree not stopped: %+v %v", result, err)
			}
		}
	})
	return p
}

func (p *processBindingPort) Create(ctx context.Context, call NativeCreate) (NativeCreation, error) {
	p.creates.Add(1)
	if p.before != nil {
		if err := p.before(ctx); err != nil {
			return NativeCreation{Outcome: "not_sent"}, err
		}
	}
	executable, err := os.Executable()
	if err != nil {
		return NativeCreation{Outcome: "not_sent"}, err
	}
	env := []string{"COORDICA_BINDING_FIXTURE=1", "COORDICA_BINDING_JOURNAL=" + p.journal, "GORACE=atexit_sleep_ms=0", "GOMAXPROCS=1"}
	for _, name := range []string{"PATH", "SystemRoot", "TMPDIR", "TMP", "TEMP"} {
		if value := os.Getenv(name); value != "" {
			env = append(env, name+"="+value)
		}
	}
	child, err := process.Start(p.life, process.Spec{Executable: executable, Args: []string{"runtime-binding-fixture"}, Dir: p.directory, Env: env, MaxLineBytes: 32 << 10})
	if err != nil {
		return NativeCreation{Outcome: "not_sent"}, err
	}
	p.mu.Lock()
	p.child = child
	p.call = call
	p.mu.Unlock()
	raw, _ := json.Marshal(call)
	if err := child.Write(append(raw, '\n')); err != nil {
		return NativeCreation{Outcome: "unknown"}, err
	}
	select {
	case event, ok := <-child.Output():
		var result NativeCreation
		if !ok || event.Stream != "stdout" || json.Unmarshal(event.Line, &result) != nil {
			return NativeCreation{Outcome: "unknown"}, errors.New("invalid fixture creation")
		}
		p.mu.Lock()
		p.id = result.ID
		p.mu.Unlock()
		return result, nil
	case <-ctx.Done():
		return NativeCreation{Outcome: "unknown"}, ctx.Err()
	}
}

func (p *processBindingPort) Stop(ctx context.Context, call NativeCreate, _ NativeSessionID) (BindingStop, error) {
	p.stops.Add(1)
	if p.before != nil {
		if err := p.before(ctx); err != nil {
			return BindingStop{}, err
		}
	}
	p.mu.Lock()
	child, id, created := p.child, p.id, p.call
	p.mu.Unlock()
	if child == nil {
		return BindingStop{BindingID: call.BindingID, OwnerID: call.OwnerID, TreeStopped: true}, nil
	}
	if call.BindingID != created.BindingID || call.OwnerID != created.OwnerID || call.Config != created.Config {
		return BindingStop{}, ErrStopUnconfirmed
	}
	child.Stop()
	for range child.Output() {
	}
	result, err := child.Wait()
	if result.TreeStopped {
		err = nil
	} // Controlled cancellation may accompany proven shutdown.
	return BindingStop{BindingID: call.BindingID, OwnerID: call.OwnerID, NativeID: id, TreeStopped: result.TreeStopped}, err
}

func TestBindingNativePortRunsOutsideTransactionAndReleaseHasRealTreeStop(t *testing.T) {
	port := newProcessBindingPort(t)
	f, p, task, run := bindingFixture(t, port)
	port.before = func(ctx context.Context) error {
		read := make(chan error, 1)
		go func() { _, err := f.ledger.ReadTask(ctx, f.cred, task.Task.ID); read <- err }()
		select {
		case err := <-read:
			return err
		case <-time.After(time.Second):
			return errors.New("native I/O held database transaction")
		}
	}
	binding := prepareFixture(t, f, p, task, run)
	ready, err := p.Produce(t.Context(), f.cred, binding.ID)
	if err != nil || ready.State != "ready" {
		t.Fatalf("fixture binding failed: %+v %v", ready, err)
	}
	journal, err := os.ReadFile(port.journal)
	if err != nil || string(journal) != binding.CommandID+"\n" {
		t.Fatalf("native create count evidence: %q %v", journal, err)
	}
	if _, err := f.ledger.CancelTask(t.Context(), f.cred, CancelRequest{Key: "premature-cancel", TaskID: task.Task.ID, ExpectedVersion: 3}); !errors.Is(err, ErrStopUnconfirmed) {
		t.Fatalf("bound resource bypassed cancel guard: %v", err)
	}
	req := BindingRequest{Key: "release", BindingID: ready.ID, ExpectedVersion: ready.Version, Generation: ready.Generation}
	released, err := p.Release(t.Context(), f.cred, req)
	if err != nil || released.State != "released" || !released.CleanupStopped {
		t.Fatalf("release lacked real process proof: %+v %v", released, err)
	}
	if _, err := p.Release(t.Context(), f.cred, req); err != nil || port.stops.Load() != 1 || port.creates.Load() != 1 {
		t.Fatalf("release replay performed I/O: %v", err)
	}
	after := readFixture(t, f, task.Task.ID)
	if after.Task.State != "open" || after.Context.Content != task.Context.Content {
		t.Fatal("release cancelled task or changed context", after)
	}
	if _, err := f.ledger.CancelRun(t.Context(), f.cred, CancelRequest{Key: "cancel-after-stop", TaskID: task.Task.ID, ExpectedVersion: 3, RunID: run.Run.ID, RunGeneration: run.Run.Generation}); err != nil {
		t.Fatalf("proven stopped local run could not cancel: %v", err)
	}
}
