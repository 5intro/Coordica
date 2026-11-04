package appserver

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func fixtureOptions() ThreadOptions {
	return ThreadOptions{Model: "fixture-model", ApprovalPolicy: "on-request", Sandbox: "read-only"}
}

func openThreadFixture(t *testing.T, scenario string) (*Peer, Thread) {
	t.Helper()
	p := openFixture(t, scenario)
	thread, config, err := StartThread(t.Context(), p, fixtureOptions())
	if err != nil || thread.ID() != "native-thread" || config.Model != "fixture-model" {
		t.Fatalf("explicit thread selection failed: %+v %v", config, err)
	}
	return p, thread
}

func TestTypedThreadTwoTurnsAndResumeNeverReplaysInput(t *testing.T) {
	p, thread := openThreadFixture(t, "typed")
	for i := 1; i <= 2; i++ {
		turn, err := thread.StartText(t.Context(), "fixture input", "fixture-effort")
		if err != nil || turn.ThreadID() != thread.ID() || turn.InitialStatus() != "inProgress" {
			t.Fatalf("turn identity lost: %+v %v", turn, err)
		}
		if nextFrame(t, p).Method != "turn/started" {
			t.Fatal("missing native start fact")
		}
		call(t, p, "fixture/finish", map[string]any{}, nil)
		if nextFrame(t, p).Method != "turn/completed" {
			t.Fatal("missing native completion fact")
		}
	}
	resumed, config, err := ResumeThread(t.Context(), p, thread.ID(), fixtureOptions())
	if err != nil || resumed.ID() != thread.ID() || config.NativeThreadID != thread.ID() {
		t.Fatalf("wrong resume target: %+v %v", config, err)
	}
	raw, err := resumed.Read(t.Context(), true)
	if err != nil || !json.Valid(raw) {
		t.Fatalf("read failed: %s %v", raw, err)
	}
	var status struct {
		Starts, Resumes, Turns int
		Effort, Text           string
	}
	call(t, p, "fixture/status", map[string]any{}, &status)
	if status.Starts != 1 || status.Resumes != 1 || status.Turns != 2 || status.Effort != "fixture-effort" || status.Text != "fixture input" {
		t.Fatalf("resume/read replayed work or dropped options: %+v", status)
	}
}

func TestTypedSteerTargetsOneTurnAndInterruptDoesNotStartAnother(t *testing.T) {
	p, thread := openThreadFixture(t, "typed")
	turn, err := thread.StartText(t.Context(), "fixture", "high")
	if err != nil {
		t.Fatal(err)
	}
	nextFrame(t, p)
	if err := turn.SteerText(t.Context(), "additional fixture context"); err != nil {
		t.Fatal(err)
	}
	if err := turn.Interrupt(t.Context()); err != nil {
		t.Fatal(err)
	}
	frame := nextFrame(t, p)
	if frame.Method != "turn/completed" {
		t.Fatal(frame)
	}
	for _, err := range []error{turn.SteerText(t.Context(), "too late"), turn.Interrupt(t.Context())} {
		var rpc *RPCError
		if !errors.As(err, &rpc) || rpc.Code != -32001 {
			t.Fatalf("stale operation was requeued: %v", err)
		}
	}
	var status struct {
		Turns  int
		Active bool
	}
	call(t, p, "fixture/status", map[string]any{}, &status)
	if status.Turns != 1 || status.Active {
		t.Fatalf("interrupt claimed another turn: %+v", status)
	}
}

func TestTypedConfigurationMismatchPreservesFactsAndStartsNoWork(t *testing.T) {
	for _, scenario := range []string{"typed-model-mismatch", "typed-directory-mismatch"} {
		t.Run(scenario, func(t *testing.T) {
			p := openFixture(t, scenario)
			thread, observed, err := StartThread(t.Context(), p, fixtureOptions())
			if !errors.Is(err, ErrConfiguration) || thread.valid() || observed.NativeThreadID != "native-thread" {
				t.Fatalf("native config mismatch hidden: %+v %+v %v", thread, observed, err)
			}
			var status struct{ Turns int }
			call(t, p, "fixture/status", map[string]any{}, &status)
			if status.Turns != 0 {
				t.Fatal("work started with different config")
			}
		})
	}
}

func TestTypedMalformedResultsStopInsteadOfReturningUsableHandles(t *testing.T) {
	for _, scenario := range []string{"typed-missing-config", "typed-wrong-resume-id", "typed-bad-turn", "typed-bad-steer"} {
		t.Run(scenario, func(t *testing.T) {
			p := openFixture(t, scenario)
			var err error
			if scenario == "typed-wrong-resume-id" {
				_, _, err = ResumeThread(t.Context(), p, "native-thread", fixtureOptions())
			} else {
				var thread Thread
				thread, _, err = StartThread(t.Context(), p, fixtureOptions())
				if err == nil {
					var turn Turn
					turn, err = thread.StartText(t.Context(), "fixture", "high")
					if err == nil {
						err = turn.SteerText(t.Context(), "fixture supplement")
					}
				}
			}
			if !errors.Is(err, ErrProtocol) || !errors.Is(err, ErrOutcomeUnknown) {
				t.Fatalf("bad result became success: %v", err)
			}
			if stopped, err := p.Close(); !stopped.TreeStopped {
				t.Fatalf("invalid peer kept running: %+v %v", stopped, err)
			}
		})
	}
}

func TestTypedInvalidSelectionIsRejectedBeforeSending(t *testing.T) {
	p := openFixture(t, "typed")
	for _, options := range []ThreadOptions{{}, {Model: "fixture-model", ApprovalPolicy: "pretend", Sandbox: "read-only"}} {
		if _, _, err := StartThread(t.Context(), p, options); !errors.Is(err, ErrConfiguration) {
			t.Fatal(err)
		}
	}
	if _, _, err := ResumeThread(t.Context(), p, "bad\nthread", fixtureOptions()); !errors.Is(err, ErrInvalidTarget) {
		t.Fatal(err)
	}
	if _, err := (Thread{}).StartText(t.Context(), "fixture", "high"); !errors.Is(err, ErrInvalidTarget) {
		t.Fatal(err)
	}
	if err := (Turn{}).Interrupt(t.Context()); !errors.Is(err, ErrInvalidTarget) {
		t.Fatal(err)
	}
	var status struct{ Starts, Resumes, Turns int }
	call(t, p, "fixture/status", map[string]any{}, &status)
	if status.Starts+status.Resumes+status.Turns != 0 {
		t.Fatalf("invalid selection reached native process: %+v", status)
	}
}

func TestTypedWorkspaceCanonicalizationDoesNotRequireGit(t *testing.T) {
	config := fixtureConfig(t, "typed")
	link := filepath.Join(t.TempDir(), "workspace-link")
	if err := os.Symlink(config.Directory, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	config.Directory = link
	p, err := Open(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if status, err := p.Close(); !status.TreeStopped {
			t.Errorf("fixture not stopped: %+v %v", status, err)
		}
	})
	_, observed, err := StartThread(t.Context(), p, fixtureOptions())
	if err != nil || observed.Directory == link {
		t.Fatalf("workspace was not canonical: %+v %v", observed, err)
	}
	if _, err := os.Stat(filepath.Join(observed.Directory, ".git")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("fixture acquired a Git requirement: %v", err)
	}
}
