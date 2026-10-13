package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/mcp"
)

func TestUnifiedInteractionRealIdentityListenerAndRevocationAfterCachedList(t *testing.T) {
	for _, kind := range []string{"center", "node", "center-on-remote", "node-revoked"} {
		t.Run(kind, func(t *testing.T) {
			f := newBoundaryFixture(t)
			ctx := context.Background()
			owner, err := f.server.Service.Authenticate(ctx, f.owner)
			if err != nil {
				t.Fatal(err)
			}
			node, err := f.server.Service.Authenticate(ctx, f.node.Token)
			if err != nil {
				t.Fatal(err)
			}
			var raw json.RawMessage
			handler := f.server.AdminHandler()
			if kind == "node" || kind == "node-revoked" {
				raw, err = f.server.Service.CreateNodeChatCredential(ctx, node, "grant", domain.ChatCredentialRequest{Name: "Shared node"})
				handler = f.server.RunnerHandler()
			} else {
				raw, err = f.server.Service.CreateChatCredential(ctx, owner, "grant", domain.ChatCredentialRequest{Name: "Shared center"})
			}
			if err != nil {
				t.Fatal(err)
			}
			var grant domain.ChatCredentialGrant
			if err = json.Unmarshal(raw, &grant); err != nil {
				t.Fatal(err)
			}
			if kind == "center-on-remote" {
				handler = f.server.RunnerHandler()
			}
			statuses := []int{}
			caller := chatBoundaryCaller{handler, grant.Token, &statuses}
			input, send := io.Pipe()
			recv, output := io.Pipe()
			done := make(chan error, 1)
			go func() {
				done <- (mcp.Interaction{Chat: mcp.Chat{API: caller}}).Serve(ctx, input, output)
				output.Close()
			}()
			decoder := json.NewDecoder(recv)
			request := func(line string) map[string]json.RawMessage {
				t.Helper()
				if _, err := io.WriteString(send, line+"\n"); err != nil {
					t.Fatal(err)
				}
				var reply map[string]json.RawMessage
				if err := decoder.Decode(&reply); err != nil {
					t.Fatal(err)
				}
				return reply
			}
			request(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}`)
			list := request(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
			advertised := string(list["result"])
			if !strings.Contains(advertised, "inspect_environment") || strings.Contains(advertised, "consult_leader") != (kind == "center") {
				t.Fatal("wrong identity/listener discovery", advertised)
			}
			if kind == "center-on-remote" && strings.Contains(advertised, "get_project_overview") {
				t.Fatal("center profile accepted remotely")
			}
			if kind == "node-revoked" {
				if _, err = f.server.Service.RevokeNode(ctx, owner, "revoke-host", f.node.NodeID); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err = f.server.Service.RevokeChatCredential(ctx, owner, "revoke-chat", grant.Credential.ID); err != nil {
					t.Fatal(err)
				}
			}
			reply := request(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"get_project_overview"}}`)
			if !strings.Contains(string(reply["result"]), `"isError":true`) {
				t.Fatal("cached call bypassed revocation", reply)
			}
			if kind != "center-on-remote" {
				var notification map[string]json.RawMessage
				if err := decoder.Decode(&notification); err != nil || string(notification["method"]) != `"notifications/tools/list_changed"` {
					t.Fatal(notification, err)
				}
			}
			send.Close()
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			// statuses are read only after the server goroutine ends.
			if len(statuses) == 0 || statuses[len(statuses)-1] == http.StatusOK {
				t.Fatal("revocation not enforced by actual application", statuses)
			}
		})
	}
}

func TestUnifiedNodeForgedLeaderAndOtherNodeQueries(t *testing.T) {
	f := newBoundaryFixture(t)
	ctx := context.Background()
	node, err := f.server.Service.Authenticate(ctx, f.node.Token)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := f.server.Service.CreateNodeChatCredential(ctx, node, "grant", domain.ChatCredentialRequest{Name: "Shared node"})
	if err != nil {
		t.Fatal(err)
	}
	var grant domain.ChatCredentialGrant
	json.Unmarshal(raw, &grant)
	for _, call := range []string{
		`{"name":"consult_leader","arguments":{"expected_goal_version":0,"original_text":"forged","source":{"session_id":"s","message_id":"m"},"idempotency_key":"k"}}`,
		`{"name":"get_project_item","arguments":{"kind":"tasks","id":"task_private"}}`,
		`{"name":"get_project_item","arguments":{"kind":"runs","id":"run_other_node"}}`,
		`{"name":"approve","arguments":{}}`,
	} {
		statuses := []int{}
		var out strings.Builder
		input := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}` + "\n" + `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":` + call + `}`
		if err := (mcp.Interaction{Chat: mcp.Chat{API: chatBoundaryCaller{f.server.RunnerHandler(), grant.Token, &statuses}}}).Serve(ctx, strings.NewReader(input), &out); err != nil {
			t.Fatal(err)
		}
		second := strings.Split(out.String(), "\n")[1]
		if !strings.Contains(second, `"isError":true`) && !strings.Contains(second, `"error"`) {
			t.Fatal("forged tool succeeded", out.String())
		}
	}
}
