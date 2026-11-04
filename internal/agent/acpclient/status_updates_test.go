package acpclient

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func statusFixture(t *testing.T, scenario string) (*Peer, Session, string) {
	t.Helper()
	config, journal := fixtureConfig(t, "status-"+scenario)
	p, err := Open(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Close() })
	s, err := p.NewSession(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return p, s, journal
}

func assertStatusMetadata(t *testing.T, snapshot UpdateSnapshot) {
	t.Helper()
	raw, _ := json.Marshal(snapshot)
	if strings.Contains(string(raw), "private-") {
		t.Fatal("status payload entered metadata", string(raw))
	}
	for _, fact := range snapshot.Facts {
		if fact.Kind == "plan" || fact.Kind == "available_commands_update" || fact.Kind == "current_mode_update" {
			if fact.TextBytes != 0 || fact.ToolCallID != "" || fact.State != "" || fact.ConfigVersion != 0 {
				t.Fatal("native declaration became output, tool state or effective configuration", fact)
			}
		}
	}
}

func assertStatusJournal(t *testing.T, journal string, methods ...string) {
	t.Helper()
	raw, err := os.ReadFile(journal)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != len(methods) {
		t.Fatalf("unexpected native operations: %s", raw)
	}
	for i, line := range lines {
		var frame struct{ Method string }
		if json.Unmarshal([]byte(line), &frame) != nil || frame.Method != methods[i] {
			t.Fatalf("native operation %d: %s", i, line)
		}
	}
}

func TestStatusUpdatesDoNotCompletePromptOrExecuteDeclarations(t *testing.T) {
	p, s, journal := statusFixture(t, "normal")
	done := startCancelable(s, t.Context(), "explicit-prompt", nil)
	snapshot := awaitUpdates(t, s, 4)
	if snapshot.Gap || snapshot.ObservedThrough != 4 || snapshot.AcceptedThrough != 4 || snapshot.Process.TreeStopped {
		t.Fatal(snapshot)
	}
	for i, kind := range []string{"plan", "available_commands_update", "current_mode_update", "agent_message_chunk"} {
		if snapshot.Facts[i].Kind != kind || snapshot.Facts[i].Sequence != int64(i+1) {
			t.Fatal("status facts lost receive order", snapshot)
		}
	}
	assertStatusMetadata(t, snapshot)
	select {
	case out := <-done:
		t.Fatal("status declaration completed prompt without original terminal", out)
	default:
	}
	if _, err := s.SetConfigOption(t.Context(), "not-selected", "value"); !errors.Is(err, ErrSessionBusy) {
		t.Fatal("mode notification ended singleflight", err)
	}
	configuration, err := s.ConfigOptions()
	if err != nil || configuration.Available || configuration.Version != 1 {
		t.Fatal("mode/commands changed configuration", configuration, err)
	}
	if _, err = p.LoadSession(t.Context(), s.ID()); !errors.Is(err, ErrUnsupported) {
		t.Fatal("status observations enabled history loading", err)
	}
	assertStatusJournal(t, journal, "initialize", "session/new", "session/prompt")
	if err = os.WriteFile(filepath.Join(filepath.Dir(journal), "continue"), []byte("terminal"), 0600); err != nil {
		t.Fatal(err)
	}
	out := awaitCancelable(t, done)
	if out.err != nil || out.result.LocalRequestID != 3 || out.result.StopReason != "end_turn" || out.result.Evidence != "rpc_terminal" || p.Err() != nil {
		t.Fatal(out, p.Err())
	}
	next, err := s.PromptText(t.Context(), "explicit-next-prompt")
	if err != nil || next.LocalRequestID != 4 || next.StopReason != "end_turn" {
		t.Fatal("status notification consumed a request ID or stopped peer", next, err)
	}
	snapshot = awaitUpdates(t, s, 8)
	assertStatusMetadata(t, snapshot)
	if snapshot.Gap || snapshot.ObservedThrough != 8 || snapshot.Facts[4].Sequence != 5 {
		t.Fatal("status facts were deduplicated or assigned native prompt IDs", snapshot)
	}
	if status, _ := p.Close(); !status.TreeStopped {
		t.Fatal("owned fixture not stopped")
	}
	assertStatusJournal(t, journal, "initialize", "session/new", "session/prompt", "session/prompt")
}

func TestStatusUpdateFailureKeepsPromptUnknownAndActualStop(t *testing.T) {
	for _, scenario := range []string{"bad-plan", "bad-command", "bad-mode", "wrong-session-plan", "wrong-session-available_commands_update", "wrong-session-current_mode_update", "oversize", "flood"} {
		t.Run(scenario, func(t *testing.T) {
			p, s, journal := statusFixture(t, scenario)
			result, err := s.PromptText(t.Context(), "explicit-prompt")
			var failure *PromptError
			if !errors.Is(err, ErrOutcomeUnknown) || !errors.As(err, &failure) || !failure.Process.TreeStopped || result.Evidence != "" {
				t.Fatal("invalid/lost status invented a prompt outcome or stop", result, err)
			}
			snapshot, readErr := s.Updates()
			if readErr != nil || !snapshot.Gap || !snapshot.Ended || !snapshot.Process.TreeStopped || p.Err() == nil {
				t.Fatal("status failure lost diagnostic facts", snapshot, readErr)
			}
			assertStatusMetadata(t, snapshot)
			if strings.Contains(err.Error(), "private-") {
				t.Fatal("error leaked status body", err)
			}
			switch scenario {
			case "flood":
				if !errors.Is(err, ErrBackpressure) || len(snapshot.Facts) != 32 || snapshot.ObservedThrough != 33 || snapshot.AcceptedThrough != 32 {
					t.Fatal("status flood escaped existing queue limit", snapshot, err)
				}
				if err = s.ReleaseUpdates(32); err != nil {
					t.Fatal(err)
				}
				after, _ := s.Updates()
				if !after.Gap || after.Reason != "backpressure" || len(after.Facts) != 0 || after.ObservedThrough != 33 {
					t.Fatal("release repaired lost status history", after)
				}
			case "oversize":
				if len(snapshot.Facts) != 1 || snapshot.Facts[0].Kind != "plan" {
					t.Fatal("oversize frame erased accepted metadata", snapshot)
				}
			default:
				if !errors.Is(err, ErrProtocol) || len(snapshot.Facts) != 0 {
					t.Fatal("invalid shape/session was silently ignored", snapshot, err)
				}
			}
			assertStatusJournal(t, journal, "initialize", "session/new", "session/prompt")
		})
	}
}

func TestStatusLargePrivatePayloadStaysInsideFrameAndOutsideMetadata(t *testing.T) {
	p, s, journal := statusFixture(t, "large")
	result, err := s.PromptText(t.Context(), "explicit-prompt")
	if err != nil || result.StopReason != "end_turn" || p.Err() != nil {
		t.Fatal(result, err)
	}
	snapshot := awaitUpdates(t, s, 4)
	assertStatusMetadata(t, snapshot)
	raw, _ := json.Marshal(snapshot)
	if snapshot.Gap || len(raw) > 2048 {
		t.Fatal("status body grew retained metadata", snapshot)
	}
	p.Close()
	assertStatusJournal(t, journal, "initialize", "session/new", "session/prompt")
}

func TestStatusUpdatesDuringNativeCancelStillWaitForOriginalTerminal(t *testing.T) {
	p, s, journal := cancelFixture(t, "status")
	signal := make(chan struct{})
	done := startCancelable(s, t.Context(), "explicit-prompt", signal)
	awaitUpdates(t, s, 2)
	close(signal)
	snapshot := awaitUpdates(t, s, 5)
	assertStatusMetadata(t, snapshot)
	if snapshot.Gap || p.Err() != nil {
		t.Fatal(snapshot, p.Err())
	}
	select {
	case out := <-done:
		t.Fatal("status update proved cancellation without prompt terminal", out)
	case <-time.After(10 * time.Millisecond):
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(journal), "continue"), []byte("terminal"), 0600); err != nil {
		t.Fatal(err)
	}
	out := awaitCancelable(t, done)
	if out.err != nil || out.result.StopReason != "cancelled" || out.result.LocalRequestID != 3 || out.result.Evidence != "rpc_terminal" || p.Err() != nil {
		t.Fatal(out, p.Err())
	}
	if next, err := s.PromptText(t.Context(), "after-cancel"); err != nil || next.LocalRequestID != 4 || next.StopReason != "end_turn" {
		t.Fatal(next, err)
	}
	p.Close()
	assertStatusJournal(t, journal, "initialize", "session/new", "session/prompt", "session/cancel", "session/prompt")
}
