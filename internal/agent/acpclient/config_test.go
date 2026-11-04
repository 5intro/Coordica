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
)

func configFixture(t *testing.T, scenario string) (*Peer, Session, string) {
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
	return p, s, journal
}

func TestConfigSnapshotsArePrivateDefensiveAndUnavailableDiffersFromEmpty(t *testing.T) {
	for _, scenario := range []string{"absent", "null", "empty"} {
		t.Run(scenario, func(t *testing.T) {
			p, s, journal := configFixture(t, "config-"+scenario)
			v, err := s.ConfigOptions()
			if err != nil || v.Available != (scenario == "empty") || len(v.Options) != 0 {
				t.Fatal(v, err)
			}
			if _, err = s.SetConfigOption(t.Context(), "native-model", "large"); !errors.Is(err, ErrConfigUnavailable) || p.Err() != nil {
				t.Fatal(err)
			}
			raw, _ := os.ReadFile(journal)
			if strings.Contains(string(raw), "set_config_option") {
				t.Fatal("unadvertised selection sent")
			}
		})
	}
	p, s, journal := configFixture(t, "config-normal")
	v, _ := s.ConfigOptions()
	v.Options[0].ID = "forged"
	*v.Options[0].Category = "forged"
	v.Options[0].Groups[0].Options[0].Value = "forged"
	*v.Options[0].Groups[0].Options[0].Description = "forged"
	actual, _ := s.ConfigOptions()
	if actual.Options[0].ID != "native-model" || *actual.Options[0].Category != "vendor_hint" || actual.Options[0].Groups[0].Options[0].Value != "small" || *actual.Options[0].Groups[0].Options[0].Description != "private-choice-description" {
		t.Fatal("snapshot aliases live state", actual)
	}
	forged := s
	forged.epoch += "other"
	if _, err := forged.ConfigOptions(); !errors.Is(err, ErrProtocol) {
		t.Fatal("foreign epoch read", err)
	}
	for _, input := range [][2]string{{"Native model", "large"}, {"vendor_hint", "large"}, {"native-model", "unknown"}} {
		if _, err := s.SetConfigOption(t.Context(), input[0], input[1]); !errors.Is(err, ErrConfigUnavailable) {
			t.Fatal(input, err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.SetConfigOption(ctx, "native-model", "large"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(journal)
	if strings.Contains(string(raw), "set_config_option") || p.Err() != nil {
		t.Fatal("local rejection wrote/damaged peer")
	}
	if result, err := s.SetConfigOption(t.Context(), "native-model", "large"); err != nil || result.LocalRequestID != 3 {
		t.Fatal(result, err)
	}
}

func TestConfigAndPromptShareOneFlightAndRequestIDSpace(t *testing.T) {
	for _, first := range []string{"config", "prompt"} {
		t.Run(first, func(t *testing.T) {
			scenario := "config-pause"
			if first == "prompt" {
				scenario = "config-prompt-pause"
			}
			p, s, journal := configFixture(t, scenario)
			done := make(chan error, 1)
			method := "session/set_config_option"
			if first == "config" {
				go func() { _, err := s.SetConfigOption(t.Context(), "native-model", "large"); done <- err }()
			} else {
				method = "session/prompt"
				go func() { _, err := s.PromptText(t.Context(), "explicit"); done <- err }()
			}
			awaitConfigJournal(t, journal, method)
			if _, err := s.SetConfigOption(t.Context(), "native-model", "large"); !errors.Is(err, ErrSessionBusy) {
				t.Fatal("second config sent", err)
			}
			if _, err := s.PromptText(t.Context(), "explicit-next"); !errors.Is(err, ErrSessionBusy) {
				t.Fatal("second prompt sent", err)
			}
			continueConfigFixture(t, journal)
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			if result, err := s.SetConfigOption(t.Context(), "native-model", "small"); err != nil || result.LocalRequestID != 4 || p.Err() != nil {
				t.Fatal(result, err)
			}
			raw, _ := os.ReadFile(journal)
			if strings.Count(string(raw), "\n") != 4 {
				t.Fatal("busy path wrote request", string(raw))
			}
		})
	}
}

func TestConfigReceiveOrderPreservesHistoricalReceiptAndLatestSnapshot(t *testing.T) {
	for _, order := range []string{"before", "after"} {
		t.Run(order, func(t *testing.T) {
			_, s, _ := configFixture(t, "config-"+order)
			result, err := s.SetConfigOption(t.Context(), "native-model", "large")
			if err != nil || !result.Confirmed {
				t.Fatal(result, err)
			}
			u := awaitUpdates(t, s, 1)
			latest, _ := s.ConfigOptions()
			want := uint64(3)
			if order == "after" {
				want = 2
			}
			if result.Observed.Version != want || result.Observed.Options[0].CurrentValue != "large" || latest.Version != 3 || u.Facts[0].Kind != "config_option_update" {
				t.Fatal(result, latest, u)
			}
			if order == "after" && (latest.Options[0].CurrentValue != "small" || u.Facts[0].ConfigVersion != 3) {
				t.Fatal("late function overwrote newer notification", result, latest, u)
			}
			result.Observed.Options[0].Groups[0].Options[0].Value = "changed"
			check, _ := s.ConfigOptions()
			if check.Options[0].Groups[0].Options[0].Value != "small" {
				t.Fatal("receipt aliases snapshot")
			}
		})
	}
}

func TestConfigValidMismatchAndKnownRejectionRemainObservedFacts(t *testing.T) {
	for _, scenario := range []string{"wrong", "empty", "rejected"} {
		t.Run(scenario, func(t *testing.T) {
			name := "config-response-" + scenario
			if scenario == "rejected" {
				name = "config-rejected"
			}
			p, s, journal := configFixture(t, name)
			result, err := s.SetConfigOption(t.Context(), "native-model", "large")
			if scenario == "rejected" {
				var rpc *RPCError
				if !errors.As(err, &rpc) || rpc.Message != "private-config-rejection" || result.Evidence != "" {
					t.Fatal(result, err)
				}
			} else if !errors.Is(err, ErrConfigMismatch) || result.Confirmed || !result.Observed.Available || result.Observed.Version != 2 {
				t.Fatal(result, err)
			}
			if errors.Is(err, ErrOutcomeUnknown) || p.Err() != nil || strings.Contains(err.Error(), "private-") {
				t.Fatal("valid mismatch/rejection became unknown or leaked", err)
			}
			if _, err := s.PromptText(t.Context(), "explicit-after-known"); err != nil {
				t.Fatal("known response killed peer", err)
			}
			raw, _ := os.ReadFile(journal)
			if strings.Count(string(raw), "set_config_option") != 1 {
				t.Fatal("implicit retry", string(raw))
			}
		})
	}
}

func TestConfigUnknownResponsesAndSentCancellationCarryActualStopProof(t *testing.T) {
	for _, scenario := range []string{"response-null", "response-missing", "response-duplicate", "wrong-id", "disconnect", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			name := "config-" + scenario
			if scenario == "cancel" {
				name = "config-no-response"
			}
			_, s, journal := configFixture(t, name)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := s.SetConfigOption(ctx, "native-model", "large"); done <- err }()
			if scenario == "cancel" {
				awaitConfigJournal(t, journal, "session/set_config_option")
				cancel()
			}
			err := <-done
			var failure *ConfigSelectionError
			if !errors.Is(err, ErrOutcomeUnknown) || !errors.As(err, &failure) || !failure.Process.TreeStopped || strings.Contains(err.Error(), "private-") {
				t.Fatal(err)
			}
			if _, err := s.SetConfigOption(t.Context(), "native-model", "large"); !errors.Is(err, ErrDisconnected) {
				t.Fatal("unknown call retried", err)
			}
			raw, _ := os.ReadFile(journal)
			if strings.Count(string(raw), "set_config_option") != 1 {
				t.Fatal(string(raw))
			}
		})
	}
}

func TestConfigTerminalObservationSurvivesEOFAndMetadataRemainsSessionScoped(t *testing.T) {
	p, s, _ := configFixture(t, "config-response-eof")
	result, err := s.SetConfigOption(t.Context(), "native-model", "large")
	if !result.Confirmed || result.Observed.Version != 2 || errors.Is(err, ErrOutcomeUnknown) {
		t.Fatal("known selection lost", result, err)
	}
	select {
	case <-p.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("EOF not observed")
	}
	if proof, _ := p.Close(); !proof.TreeStopped {
		t.Fatal(proof)
	}
	current, _ := s.ConfigOptions()
	u, _ := s.Updates()
	if current.Options[0].CurrentValue != "large" || !u.Gap || !u.Ended || !u.Process.TreeStopped {
		t.Fatal(current, u)
	}
}

func TestConfigNotificationsAreWholeSnapshotsAndGapIsSticky(t *testing.T) {
	for _, scenario := range []string{"empty", "null", "missing", "overflow"} {
		t.Run(scenario, func(t *testing.T) {
			p, s, journal := configFixture(t, "config-notify-"+scenario)
			continueConfigFixture(t, journal)
			if scenario == "empty" {
				u := awaitUpdates(t, s, 1)
				current, _ := s.ConfigOptions()
				if !current.Available || len(current.Options) != 0 || current.Version != 2 || u.Facts[0].ConfigVersion != 2 {
					t.Fatal(current, u)
				}
				return
			}
			select {
			case <-p.Done():
			case <-time.After(3 * time.Second):
				t.Fatal("invalid/overflow notification did not close")
			}
			if proof, _ := p.Close(); !proof.TreeStopped {
				t.Fatal(proof)
			}
			u, _ := s.Updates()
			if !u.Gap || !u.Ended || !u.Process.TreeStopped {
				t.Fatal(u)
			}
			if scenario == "overflow" && (len(u.Facts) != 32 || u.ObservedThrough != 33) {
				t.Fatal(u)
			}
			if err := s.ReleaseUpdates(u.AcceptedThrough); err != nil {
				t.Fatal(err)
			}
			again, _ := s.Updates()
			if !again.Gap {
				t.Fatal("release repaired gap")
			}
		})
	}
}

func awaitConfigJournal(t *testing.T, journal, method string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		raw, _ := os.ReadFile(journal)
		if strings.Contains(string(raw), method) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("native request not observed", method)
}
func continueConfigFixture(t *testing.T, journal string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(filepath.Dir(journal), "continue"), []byte("continue"), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestConfigSelectionThenExplicitPromptRetainsNativeReportedFacts(t *testing.T) {
	p, s, journal := configFixture(t, "config-normal")
	initial, err := s.ConfigOptions()
	if err != nil || !initial.Available || initial.Version != 1 || initial.Options[0].CurrentValue != "small" {
		t.Fatal(initial, err)
	}
	selection, err := s.SetConfigOption(t.Context(), "native-model", "large")
	if err != nil || !selection.Confirmed || selection.LocalRequestID != 3 || selection.AdvertisedVersion != 1 || selection.Observed.Version != 2 || selection.Evidence != "native_reported" || selection.Observed.Options[0].CurrentValue != "large" {
		t.Fatal(selection, err)
	}
	terminal, err := s.PromptText(t.Context(), "explicit-work")
	if err != nil || terminal.LocalRequestID != 4 || terminal.StopReason != "end_turn" || p.Err() != nil {
		t.Fatal(terminal, err)
	}
	if proof, _ := p.Close(); !proof.TreeStopped {
		t.Fatal("owned process did not stop")
	}
	raw, _ := os.ReadFile(journal)
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 4 {
		t.Fatal(string(raw))
	}
	var frame struct {
		Method string
		Params map[string]json.RawMessage
	}
	if json.Unmarshal([]byte(lines[2]), &frame) != nil || frame.Method != "session/set_config_option" || string(frame.Params["configId"]) != `"native-model"` || string(frame.Params["value"]) != `"large"` {
		t.Fatal(string(raw))
	}
	if strings.Contains(string(raw), "authenticate") || strings.Contains(string(raw), "session/load") {
		t.Fatal("implicit operation", string(raw))
	}
}
