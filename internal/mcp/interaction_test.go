package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"coordica/internal/chatprofile"
	"coordica/internal/localfile"
	"coordica/internal/nodeservice"
	"coordica/internal/onboarding"
	"coordica/internal/runner"
)

func TestInteractionMissingUnsafeAndOfflineProfileKeepsLocalTools(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "shared.json")
	for _, state := range []string{"missing", "invalid", "unsafe", "offline"} {
		t.Run(state, func(t *testing.T) {
			os.Remove(path)
			switch state {
			case "invalid":
				os.WriteFile(path, []byte(`{"token":"owner-token"}`), 0600)
			case "unsafe":
				os.WriteFile(path, []byte(`{}`), 0644)
			case "offline":
				raw, _ := json.Marshal(chatprofile.Profile{CenterURL: "http://127.0.0.1:1", CredentialID: "client_" + strings.Repeat("a", 32), Token: "chat_" + strings.Repeat("a", 32)})
				if err := localfile.WritePrivate(path, raw); err != nil {
					t.Fatal(err)
				}
			}
			var out bytes.Buffer
			input := strings.Join([]string{bootstrapInitialize, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"consult_leader","arguments":{}}}`}, "\n")
			if err := (Interaction{Chat: Chat{API: ProfileCaller{Path: path}}}).Serve(context.Background(), strings.NewReader(input), &out); err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(strings.TrimSpace(out.String()), "\n")
			if len(lines) != 3 || !strings.Contains(lines[1], "initialize_runner") || strings.Contains(lines[1], "consult_leader") || !strings.Contains(lines[2], `"isError":true`) || strings.Contains(out.String(), "owner-token") {
				t.Fatal(out.String())
			}
		})
	}
}

// Pipe-driven client activity changes readiness in one live stdio process.
func TestInteractionActivityNotifiesCapabilityChangesAndCachedCallsFailClosed(t *testing.T) {
	input, send := io.Pipe()
	recv, output := io.Pipe()
	done := make(chan error, 1)
	api := &changingChatAPI{}
	go func() {
		done <- (Interaction{Chat: Chat{API: api}}).Serve(context.Background(), input, output)
		output.Close()
	}()
	decoder := json.NewDecoder(recv)
	request := func(raw string) map[string]json.RawMessage {
		t.Helper()
		if _, err := io.WriteString(send, raw+"\n"); err != nil {
			t.Fatal(err)
		}
		var reply map[string]json.RawMessage
		if err := decoder.Decode(&reply); err != nil {
			t.Fatal(err)
		}
		return reply
	}
	request(bootstrapInitialize)
	first := request(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	if strings.Contains(string(first["result"]), "consult_leader") {
		t.Fatal(first)
	}
	// Updating on the pipe writer happens before the reader's next request.
	api.set("center")
	request(`{"jsonrpc":"2.0","id":3,"method":"ping"}`)
	var notification map[string]json.RawMessage
	if err := decoder.Decode(&notification); err != nil || string(notification["method"]) != `"notifications/tools/list_changed"` {
		t.Fatal(notification, err)
	}
	list := request(`{"jsonrpc":"2.0","id":4,"method":"tools/list"}`)
	if !strings.Contains(string(list["result"]), "consult_leader") {
		t.Fatal(list)
	}
	api.set("node")
	reply := request(`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"consult_leader","arguments":{}}}`)
	if !strings.Contains(string(reply["result"]), `"isError":true`) {
		t.Fatal(reply)
	}
	if err := decoder.Decode(&notification); err != nil {
		t.Fatal(err)
	}
	api.set("")
	reply = request(`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"get_project_overview"}}`)
	if !strings.Contains(string(reply["result"]), `"isError":true`) {
		t.Fatal(reply)
	}
	if err := decoder.Decode(&notification); err != nil {
		t.Fatal(err)
	}
	send.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// Synchronize test changes through a mutex; stdio discovery can run immediately
// after a response while the client prepares its next request.
type changingChatAPI struct {
	mu   sync.Mutex
	next string
}

func (f *changingChatAPI) set(value string) { f.mu.Lock(); defer f.mu.Unlock(); f.next = value }
func (f *changingChatAPI) Do(_ context.Context, _ string, path, _ string, _ any) (json.RawMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if path != "/api/v1/chat/identity" {
		return nil, errors.New("forged call reached online operation")
	}
	return json.Marshal(map[string]any{"chat_scope": f.next})
}

type interactionInstaller struct{}

func (interactionInstaller) EnsureStarted(context.Context, nodeservice.Spec) (nodeservice.Status, error) {
	return nodeservice.Status{State: "activation_requested"}, nil
}
func TestBootstrapChatPartialRetryRetainsActualSavedRegistration(t *testing.T) {
	bootstrapFixtures(t, "git")
	dir := t.TempDir()
	cfg := runner.Config{CenterURL: "http://127.0.0.1:8080", DataDir: dir, NodeID: "node_saved", Token: "node-private-token", Capacity: 1}
	raw, _ := json.Marshal(cfg)
	if err := localfile.WritePrivate(filepath.Join(dir, "runner.json"), raw); err != nil {
		t.Fatal(err)
	}
	calls := 0
	b := Bootstrap{DataDir: dir, initialize: func(ctx context.Context, c onboarding.Config) (onboarding.Result, error) {
		return onboarding.InitializeWith(ctx, c, interactionInstaller{})
	}, ProvisionChat: func(context.Context) error {
		calls++
		if calls == 1 {
			return errors.New("private chat error")
		}
		return nil
	}}
	replies := bootstrapReplies(t, b, bootstrapInitialize, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"initialize_runner","arguments":{"center_url":"http://127.0.0.1:8080"}}}`, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"initialize_runner","arguments":{"center_url":"http://127.0.0.1:8080"}}}`)
	if calls != 2 || !replies[1].Result.IsError || replies[2].Result.IsError || !strings.Contains(replies[1].Result.Content[0].Text, "interaction_chat_pending") || !strings.Contains(replies[2].Result.Content[0].Text, "existing_registration_reused") {
		t.Fatal(replies, calls)
	}
	if strings.Contains(replies[1].Result.Content[0].Text, "interaction_chat_ready") || !strings.Contains(replies[2].Result.Content[0].Text, "interaction_chat_ready") {
		t.Fatal("chat readiness did not reflect provisioning outcome", replies)
	}
	saved, err := runner.Load(dir)
	if err != nil || saved.NodeID != cfg.NodeID || saved.Token != cfg.Token {
		t.Fatal("paired identity changed", err)
	}
	for _, reply := range replies {
		raw, _ := json.Marshal(reply)
		if strings.Contains(string(raw), cfg.Token) || strings.Contains(string(raw), "private chat error") {
			t.Fatal("secret/diagnostic leak", string(raw))
		}
	}
}

func TestAllRolesStrictFramingAndNoCrossRoleDispatch(t *testing.T) {
	for _, role := range []string{"bootstrap", "chat", "run", "interaction"} {
		t.Run(role, func(t *testing.T) {
			var out bytes.Buffer
			input := strings.Join([]string{bootstrapInitialize, `{"jsonrpc":"2.0","id":2,"method":"ping","extra":true}`, `{"jsonrpc":"2.0","id":3,"method":"ping","params":{"_meta":{"x":1,"x":2}}}`, `{"jsonrpc":"2.0","id":4,"method":"ping"} {}`, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"get_context","name":"inspect_environment"}}`, `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"approve"}}`}, "\n")
			var err error
			switch role {
			case "bootstrap":
				err = (Bootstrap{}).Serve(context.Background(), strings.NewReader(input), &out)
			case "chat":
				err = (Chat{API: failingChatAPI{}}).Serve(context.Background(), strings.NewReader(input), &out)
			case "run":
				err = (Server{API: &fakeCaller{}, RunID: "fixed", Generation: 9}).Serve(context.Background(), strings.NewReader(input), &out)
			case "interaction":
				err = (Interaction{}).Serve(context.Background(), strings.NewReader(input), &out)
			}
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(strings.TrimSpace(out.String()), "\n")
			if len(lines) != 6 {
				t.Fatal(out.String())
			}
			for _, line := range lines[1:] {
				if !strings.Contains(line, `"error"`) {
					t.Fatal(line)
				}
			}
		})
	}
	f := &fakeCaller{}
	var out bytes.Buffer
	input := strings.Join([]string{bootstrapInitialize, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"initialize_runner"}}`, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"get_project_overview"}}`}, "\n")
	if err := (Server{API: f, RunID: "fixed", Generation: 9}).Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	if len(f.paths) != 0 || strings.Contains(strings.Split(out.String(), "\n")[1], "initialize_runner") {
		t.Fatal("Run cross-role dispatch", out.String())
	}
}
