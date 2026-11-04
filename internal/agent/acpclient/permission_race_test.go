package acpclient

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"coordica/internal/process"
)

type permissionRaceReply struct {
	receipt PermissionReceipt
	err     error
}

func permissionRaceFixture(t *testing.T, scenario string) (*Peer, Session, string) {
	t.Helper()
	config, journal := fixtureConfig(t, "permission-race-"+scenario)
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

func awaitPermissionRaceRequest(t *testing.T, s Session) PermissionRequest {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		requests, err := s.PermissionRequests()
		if err != nil {
			t.Fatal(err)
		}
		if len(requests) == 1 {
			return requests[0]
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("native permission was not captured")
	return PermissionRequest{}
}

func awaitPermissionWriteAttempt(t *testing.T, p *Peer) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		p.mu.Lock()
		attempted := len(p.permissions.requests) == 1 && p.permissions.requests[0].attempted
		p.mu.Unlock()
		if attempted {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("permission reply did not reach the blocked writer")
}

func awaitPermissionRaceReply(t *testing.T, replies <-chan permissionRaceReply) permissionRaceReply {
	t.Helper()
	select {
	case reply := <-replies:
		return reply
	case <-time.After(10 * time.Second):
		t.Fatal("permission writer was not reaped")
		return permissionRaceReply{}
	}
}

func blockPermissionReply(t *testing.T, p *Peer, s Session, ctx context.Context) (PermissionRequest, <-chan error, <-chan permissionRaceReply) {
	t.Helper()
	request := awaitPermissionRaceRequest(t, s)
	filler := make(chan error, 1)
	go func() { filler <- p.process.Write([]byte(strings.Repeat(" ", 1<<20))) }()
	awaitUpdates(t, s, 1)
	replies := make(chan permissionRaceReply, 1)
	go func() {
		receipt, err := s.SelectPermission(ctx, request, "allow")
		replies <- permissionRaceReply{receipt, err}
	}()
	awaitPermissionWriteAttempt(t, p)
	return request, filler, replies
}

func awaitPermissionFiller(t *testing.T, filler <-chan error) {
	t.Helper()
	select {
	case <-filler:
	case <-time.After(5 * time.Second):
		t.Fatal("owned filler write survived cleanup")
	}
}

func TestPermissionCancelCannotOvertakeClaimedSelectedWriter(t *testing.T) {
	p, s, journal := permissionRaceFixture(t, "cancel-order")
	signal := make(chan struct{})
	prompt := startCancelable(s, t.Context(), "explicit-work", signal)
	request, filler, replies := blockPermissionReply(t, p, s, t.Context())
	close(signal)
	awaitCancelState(t, p, func(p *pendingPrompt) bool { return p.cancelWriteDone != nil })
	p.mu.Lock()
	attempted := p.prompt.cancelAttempted
	p.mu.Unlock()
	if attempted {
		t.Fatal("native cancellation overtook an earlier claimed selection")
	}
	if _, err := s.SelectPermission(t.Context(), request, "reject"); !errors.Is(err, ErrStalePermission) {
		t.Fatal("opposite answer raced cancel/once-only claim", err)
	}
	if _, err := s.PromptText(t.Context(), "must-not-start"); !errors.Is(err, ErrPromptBusy) {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(journal), "continue"), []byte("drain"), 0600); err != nil {
		t.Fatal(err)
	}
	reply := awaitPermissionRaceReply(t, replies)
	if reply.err != nil || !reply.receipt.Written || reply.receipt.Evidence != "local_write" || reply.receipt.Outcome != "selected" {
		t.Fatal(reply)
	}
	out := awaitCancelable(t, prompt)
	if out.err != nil || out.result.StopReason != "cancelled" || p.Err() != nil {
		t.Fatal("fixture verified selected-before-cancel order", out, p.Err())
	}
	awaitPermissionFiller(t, filler)
	if next, err := s.PromptText(t.Context(), "after-permission"); err != nil || next.LocalRequestID != 4 {
		t.Fatal(next, err)
	}
	p.Close()
	raw, _ := os.ReadFile(journal)
	if strings.Count(string(raw), `"outcome":"selected"`) != 1 || strings.Count(string(raw), "session/cancel") != 1 {
		t.Fatal("selection/cancel was duplicated", string(raw))
	}
}

func TestPermissionBlockedWriterKeepsKnownPromptTerminalAndSingleflight(t *testing.T) {
	p, s, journal := permissionRaceFixture(t, "terminal-block")
	ctx, stop := context.WithCancel(t.Context())
	defer stop()
	prompt := startCancelable(s, ctx, "explicit-work", nil)
	request, filler, replies := blockPermissionReply(t, p, s, t.Context())
	if err := os.WriteFile(filepath.Join(filepath.Dir(journal), "continue"), []byte("terminal"), 0600); err != nil {
		t.Fatal(err)
	}
	awaitCancelState(t, p, func(p *pendingPrompt) bool { return p.ready })
	if _, err := s.SelectPermission(t.Context(), request, "allow"); !errors.Is(err, ErrStalePermission) {
		t.Fatal("terminal allowed another answer", err)
	}
	select {
	case out := <-prompt:
		t.Fatal("known terminal released singleflight with a blocked permission writer", out)
	default:
	}
	if _, err := s.SetConfigOption(t.Context(), "none", "value"); !errors.Is(err, ErrSessionBusy) {
		t.Fatal(err)
	}
	stop()
	out := awaitCancelable(t, prompt)
	var promptFailure *PromptError
	if out.result.StopReason != "end_turn" || out.result.Evidence != "rpc_terminal" || !errors.Is(out.err, ErrDisconnected) ||
		errors.Is(out.err, ErrOutcomeUnknown) || !errors.As(out.err, &promptFailure) || !promptFailure.Process.TreeStopped {
		t.Fatal("permission write failure erased the known original terminal", out)
	}
	reply := awaitPermissionRaceReply(t, replies)
	var replyFailure *PermissionReplyError
	if reply.receipt.Written || !errors.As(reply.err, &replyFailure) || !replyFailure.Process.TreeStopped || !errors.Is(reply.err, ErrOutcomeUnknown) {
		t.Fatal("uncertain permission reply lacked actual cleanup", reply)
	}
	awaitPermissionFiller(t, filler)
	raw, _ := os.ReadFile(journal)
	if strings.Contains(string(raw), `"outcome":"selected"`) {
		t.Fatal("blocked selection was not discarded on cleanup", string(raw))
	}
}

func TestPermissionReplyContextCancelAndConcurrentCloseReapAllWriters(t *testing.T) {
	p, s, journal := permissionRaceFixture(t, "select-timeout")
	signal := make(chan struct{})
	prompt := startCancelable(s, t.Context(), "explicit-work", signal)
	ctx, stop := context.WithCancel(t.Context())
	defer stop()
	_, filler, replies := blockPermissionReply(t, p, s, ctx)
	close(signal)
	awaitCancelState(t, p, func(p *pendingPrompt) bool { return p.cancelWriteDone != nil })
	closed := make(chan process.Result, 4)
	for i := 0; i < cap(closed); i++ {
		go func() { status, _ := p.Close(); closed <- status }()
	}
	stop()
	reply := awaitPermissionRaceReply(t, replies)
	var failure *PermissionReplyError
	if reply.receipt.Written || !errors.Is(reply.err, ErrOutcomeUnknown) || !errors.As(reply.err, &failure) || !failure.Process.TreeStopped {
		t.Fatal(reply)
	}
	out := awaitCancelable(t, prompt)
	if out.result.Evidence != "" || !errors.Is(out.err, ErrOutcomeUnknown) {
		t.Fatal(out)
	}
	awaitPermissionFiller(t, filler)
	for i := 0; i < cap(closed); i++ {
		select {
		case status := <-closed:
			if !status.TreeStopped {
				t.Fatal(status)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("Close deadlocked with permission/cancel writers")
		}
	}
	raw, _ := os.ReadFile(journal)
	var frames []map[string]json.RawMessage
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var frame map[string]json.RawMessage
		if json.Unmarshal([]byte(line), &frame) != nil {
			t.Fatal("invalid fixture journal")
		}
		frames = append(frames, frame)
	}
	if len(frames) != 3 {
		t.Fatal("blocked selection/cancel escaped cleanup", string(raw))
	}
}
