package appserver

import (
	"encoding/json"
	"testing"
)

// Exercise every configuration the typed API advertises, over owned pipes.
// Selecting these fixture strings never applies a real OS permission mode.
func TestTypedSupportedThreadConfigurations(t *testing.T) {
	for _, approval := range []string{"untrusted", "on-request", "never"} {
		for _, sandbox := range [][2]string{{"read-only", "readOnly"}, {"workspace-write", "workspaceWrite"}, {"danger-full-access", "dangerFullAccess"}} {
			t.Run(approval+"/"+sandbox[0], func(t *testing.T) {
				p := openFixture(t, "typed")
				options := ThreadOptions{Model: "fixture-model", ApprovalPolicy: approval, Sandbox: sandbox[0]}
				thread, config, err := StartThread(t.Context(), p, options)
				if err != nil || !thread.valid() || config.NativeThreadID != thread.ID() {
					t.Fatalf("supported start failed: %+v %v", config, err)
				}
				resumed, observed, err := ResumeThread(t.Context(), p, thread.ID(), options)
				if err != nil || resumed.ID() != thread.ID() || observed.NativeThreadID != thread.ID() {
					t.Fatalf("supported resume failed: %+v %v", observed, err)
				}
				for _, configuration := range []Configuration{config, observed} {
					var gotApproval string
					var gotSandbox struct{ Type string }
					if json.Unmarshal(configuration.ApprovalPolicy, &gotApproval) != nil ||
						json.Unmarshal(configuration.Sandbox, &gotSandbox) != nil ||
						gotApproval != approval || gotSandbox.Type != sandbox[1] {
						t.Fatalf("native selection changed: %+v", configuration)
					}
				}
				var status struct{ Starts, Resumes, Turns int }
				call(t, p, "fixture/status", map[string]any{}, &status)
				if status.Starts != 1 || status.Resumes != 1 || status.Turns != 0 {
					t.Fatalf("configuration selection started or replayed work: %+v", status)
				}
			})
		}
	}
}

func TestTypedReadForwardsBothIncludeTurnsValuesWithoutWork(t *testing.T) {
	p, thread := openThreadFixture(t, "typed")
	for _, includeTurns := range []bool{false, true} {
		raw, err := thread.Read(t.Context(), includeTurns)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil || string(fields["projectId"]) != "null" ||
			string(fields["sessionId"]) != `"native-session"` {
			t.Fatalf("read discarded native fields or nullable values: %s %v", raw, err)
		}
	}
	var status struct {
		Starts, Resumes, Turns            int
		ReadsWithTurns, ReadsWithoutTurns int
	}
	call(t, p, "fixture/status", map[string]any{}, &status)
	if status.Starts != 1 || status.Resumes != 0 || status.Turns != 0 || status.ReadsWithTurns != 1 || status.ReadsWithoutTurns != 1 {
		t.Fatalf("read changed native work or dropped includeTurns: %+v", status)
	}
}

func TestTypedStartPreservesEveryNativeTurnStatus(t *testing.T) {
	for _, status := range []string{"inProgress", "completed", "interrupted", "failed"} {
		t.Run(status, func(t *testing.T) {
			p, thread := openThreadFixture(t, "typed-status:"+status)
			turn, err := thread.StartText(t.Context(), "fixture input", "fixture-effort")
			if err != nil || turn.ID() != "turn-1" || turn.ThreadID() != thread.ID() || turn.InitialStatus() != status {
				t.Fatalf("native turn status changed: %+v %v", turn, err)
			}
			var observed struct {
				Turns  int
				Effort string
			}
			call(t, p, "fixture/status", map[string]any{}, &observed)
			if observed.Turns != 1 || observed.Effort != "fixture-effort" {
				t.Fatalf("native status caused retry or effort fallback: %+v", observed)
			}
		})
	}
}
