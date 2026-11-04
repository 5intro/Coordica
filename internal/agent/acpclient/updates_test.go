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

func awaitUpdates(t *testing.T, s Session, count int) UpdateSnapshot {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, err := s.Updates()
		if err != nil {
			t.Fatal(err)
		}
		if len(snapshot.Facts) >= count {
			return snapshot
		}
		if snapshot.Ended {
			t.Fatal("update source ended early", snapshot)
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("updates were not captured")
	return UpdateSnapshot{}
}

func TestSessionUpdatesKeepDuplicatesAndPrivateMetadataOnly(t *testing.T) {
	config, journal := fixtureConfig(t, "updates")
	p, err := Open(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	s, err := p.NewSession(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	first := awaitUpdates(t, s, 4)
	if first.Epoch == "" || first.Evidence != "local_observed" || first.SessionID != s.ID() || first.Gap || first.ObservedThrough != 4 || first.AcceptedThrough != 4 {
		t.Fatal(first)
	}
	if first.Facts[0].Sequence != 1 || first.Facts[1].Sequence != 2 || first.Facts[0].TextBytes != int64(len("private-update-body-秘密")) || first.Facts[1].TextBytes != first.Facts[0].TextBytes || first.Facts[3].State != "" {
		t.Fatal("duplicates/null update status were invented/dropped", first)
	}
	raw, _ := json.Marshal(first)
	if strings.Contains(string(raw), "private-") {
		t.Fatal("private text/title entered metadata")
	}
	first.Facts[0].ToolCallID = "mutated"
	again, err := s.Updates()
	if err != nil || again.Facts[0].ToolCallID != "" {
		t.Fatal("snapshot aliases internal buffer", again, err)
	}
	if err = s.ReleaseUpdates(5); !errors.Is(err, ErrUpdatePosition) || p.Err() != nil {
		t.Fatal("bad local position affected Peer", err)
	}
	if err = s.ReleaseUpdates(4); err != nil {
		t.Fatal(err)
	}
	if err = s.ReleaseUpdates(4); err != nil {
		t.Fatal(err)
	}
	after, err := s.Updates()
	if err != nil || len(after.Facts) != 0 || after.ReleasedThrough != 4 || after.Gap {
		t.Fatal(after, err)
	}
	status, _ := p.Close()
	if !status.TreeStopped {
		t.Fatal("local child did not stop")
	}
	ended, err := s.Updates()
	if err != nil || !ended.Ended || !ended.Process.TreeStopped || !ended.Gap || ended.ReleasedThrough != 4 {
		t.Fatal("closed capture lost diagnostic/actual proof", ended, err)
	}
	journalRaw, err := os.ReadFile(journal)
	if err != nil || strings.Count(string(journalRaw), "\n") != 2 || strings.Contains(string(journalRaw), "session/prompt") || strings.Contains(string(journalRaw), "session/load") {
		t.Fatalf("capture sent native operations: %s %v", journalRaw, err)
	}
}

func TestToolStatusAbsenceIsDistinctFromExplicitEmpty(t *testing.T) {
	for _, kind := range []string{"tool_call", "tool_call_update"} {
		t.Run(kind, func(t *testing.T) {
			fields, err := object([]byte(`{"sessionUpdate":"` + kind + `","toolCallId":"tool","title":"private-title","status":""}`))
			if err != nil {
				t.Fatal(err)
			}
			if _, err = updateFact(fields); !errors.Is(err, ErrProtocol) {
				t.Fatal("explicit empty native status was accepted as an absent change")
			}
		})
	}
}

func startUpdateFixture(t *testing.T, scenario string) (*Peer, Session, string) {
	t.Helper()
	config, journal := fixtureConfig(t, scenario)
	p, err := Open(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Close() })
	s, err := p.NewSession(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(filepath.Dir(journal), "continue"), []byte("continue"), 0600); err != nil {
		t.Fatal(err)
	}
	return p, s, journal
}

func TestUnsupportedMalformedOrWrongSessionUpdatesKeepGapAndActualStop(t *testing.T) {
	for _, scenario := range []string{"wrong-session", "unsupported", "image", "null-content", "null-text", "null-tool-status", "bad-tag", "permission", "disconnect"} {
		t.Run(scenario, func(t *testing.T) {
			p, s, journal := startUpdateFixture(t, "updates-"+scenario)
			select {
			case <-p.Done():
			case <-time.After(3 * time.Second):
				t.Fatal("bad update source was not fenced")
			}
			snapshot, err := s.Updates()
			if err != nil || !snapshot.Gap || !snapshot.Ended || !snapshot.Process.TreeStopped || len(snapshot.Facts) != 0 || p.Err() == nil {
				t.Fatal("bad source lost gap/process proof", snapshot, err)
			}
			raw, _ := json.Marshal(snapshot)
			if strings.Contains(string(raw), "private-") || strings.Contains(p.Err().Error(), "private-") {
				t.Fatal("bad update leaked private body/id/error")
			}
			journalRaw, _ := os.ReadFile(journal)
			if strings.Count(string(journalRaw), "\n") != 2 {
				t.Fatal("capture replied/started native work")
			}
			if scenario == "unsupported" || scenario == "image" || scenario == "permission" {
				if !errors.Is(p.Err(), ErrUnsupportedUpdate) {
					t.Fatal("unsupported operation misrepresented", p.Err())
				}
			}
		})
	}
}

func TestSessionUpdateBackpressureIsBoundedStickyAndNeverNativeACK(t *testing.T) {
	for _, scenario := range []string{"flood", "byte-flood"} {
		t.Run(scenario, func(t *testing.T) {
			p, s, journal := startUpdateFixture(t, "updates-"+scenario)
			select {
			case <-p.Done():
			case <-time.After(3 * time.Second):
				t.Fatal("slow consumer blocked control/cleanup")
			}
			snapshot, err := s.Updates()
			if err != nil || !snapshot.Gap || snapshot.Reason != "backpressure" || !snapshot.Process.TreeStopped || !errors.Is(p.Err(), ErrBackpressure) || len(snapshot.Facts) > 32 || snapshot.ObservedThrough != snapshot.AcceptedThrough+1 {
				t.Fatal(snapshot, err, p.Err())
			}
			if scenario == "flood" && (len(snapshot.Facts) != 32 || snapshot.ObservedThrough != 33) {
				t.Fatal("count bound not enforced", snapshot)
			}
			if scenario == "byte-flood" && len(snapshot.Facts) >= 32 {
				t.Fatal("metadata byte bound not enforced", snapshot)
			}
			if err = s.ReleaseUpdates(snapshot.AcceptedThrough); err != nil {
				t.Fatal(err)
			}
			after, err := s.Updates()
			if err != nil || len(after.Facts) != 0 || !after.Gap || after.Reason != snapshot.Reason || after.ObservedThrough != snapshot.ObservedThrough {
				t.Fatal("release repaired a gap/observation", after, err)
			}
			raw, _ := os.ReadFile(journal)
			if strings.Count(string(raw), "\n") != 2 {
				t.Fatal("release sent native ACK")
			}
		})
	}
}

func TestUpdateCaptureScopeOrderAndOptionalStatus(t *testing.T) {
	t.Run("early", func(t *testing.T) {
		config, _ := fixtureConfig(t, "updates-early")
		p, err := Open(t.Context(), config)
		if err != nil {
			t.Fatal(err)
		}
		defer p.Close()
		_, err = p.NewSession(t.Context())
		var failure *SessionCreationError
		if !errors.Is(err, ErrUnsupportedUpdate) || !errors.As(err, &failure) || !failure.Process.TreeStopped {
			t.Fatal("unconfirmed first Session ID was guessed", err)
		}
	})
	t.Run("optional", func(t *testing.T) {
		p, s, _ := startUpdateFixture(t, "updates-optional")
		snapshot := awaitUpdates(t, s, 2)
		if snapshot.Facts[0].State != "" || snapshot.Facts[1].State != "" || p.Err() != nil {
			t.Fatal("absent/null status invented a transition", snapshot, p.Err())
		}
	})
	t.Run("epoch", func(t *testing.T) {
		config, _ := fixtureConfig(t, "session")
		p1, err := Open(t.Context(), config)
		if err != nil {
			t.Fatal(err)
		}
		defer p1.Close()
		s1, err := p1.NewSession(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		p2, err := Open(t.Context(), config)
		if err != nil {
			t.Fatal(err)
		}
		defer p2.Close()
		s2, err := p2.NewSession(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		a, _ := s1.Updates()
		b, _ := s2.Updates()
		if a.Epoch == b.Epoch {
			t.Fatal("new Peer reused source epoch")
		}
		forged := s1
		forged.peer = p2
		if _, err = forged.Updates(); !errors.Is(err, ErrProtocol) {
			t.Fatal("other Peer accepted prior private handle")
		}
	})
	t.Run("concurrent-close", func(t *testing.T) {
		config, _ := fixtureConfig(t, "session")
		p, err := Open(t.Context(), config)
		if err != nil {
			t.Fatal(err)
		}
		defer p.Close()
		s, err := p.NewSession(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		stop, done := make(chan struct{}), make(chan error, 1)
		go func() {
			for {
				if _, err := s.Updates(); err != nil {
					done <- err
					return
				}
				select {
				case <-stop:
					done <- nil
					return
				default:
				}
			}
		}()
		status, _ := p.Close()
		close(stop)
		if err = <-done; err != nil || !status.TreeStopped {
			t.Fatal("capture and Close deadlocked/lost ownership", err)
		}
	})
}
