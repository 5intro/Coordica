package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"

	"coordica/internal/domain"
	"coordica/internal/jsoninput"
)

// Chat is a thin, closed stdio client. The API and credential are fixed at
// startup; no call can choose a URL, file, process, Run or owner operation.
type Chat struct{ API Caller }

func (s Chat) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	scan := bufio.NewScanner(in)
	scan.Buffer(make([]byte, 4096), 64<<10)
	enc := json.NewEncoder(out)
	initialized := false
	for scan.Scan() {
		if e := ctx.Err(); e != nil {
			return e
		}
		var r request
		reply := map[string]any{"jsonrpc": "2.0", "id": json.RawMessage("null")}
		fail := func(code int, msg string) { reply["error"] = map[string]any{"code": code, "message": msg} }
		if jsoninput.Decode(scan.Bytes(), &r, 64<<10) != nil || r.JSONRPC != "2.0" || r.Method == "" || len(r.ID) > 0 && !validRPCID(r.ID) {
			fail(-32600, "Invalid request")
		} else if len(r.ID) == 0 {
			continue
		} else {
			reply["id"] = r.ID
			switch r.Method {
			case "initialize":
				var p struct {
					ProtocolVersion string                     `json:"protocolVersion"`
					Capabilities    map[string]json.RawMessage `json:"capabilities"`
					ClientInfo      map[string]json.RawMessage `json:"clientInfo,omitempty"`
					Meta            map[string]json.RawMessage `json:"_meta,omitempty"`
				}
				if bootstrapStrict(r.Params, &p) != nil || p.ProtocolVersion == "" || p.ClientInfo != nil && !bootstrapClientInfo(p.ClientInfo) {
					fail(-32602, "Invalid initialization parameters")
					break
				}
				initialized = true
				reply["result"] = map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]string{"name": "coordica-chat", "version": "0.1.0"}}
			case "ping", "tools/list":
				if len(r.Params) > 0 {
					var p struct {
						Meta map[string]json.RawMessage `json:"_meta,omitempty"`
					}
					if bootstrapStrict(r.Params, &p) != nil {
						fail(-32602, "Invalid parameters")
						break
					}
				}
				if r.Method == "ping" {
					reply["result"] = map[string]any{}
				} else if !initialized {
					fail(-32000, "Initialize first")
				} else {
					raw, e := s.API.Do(ctx, "GET", "/api/v1/chat/identity", "", nil)
					var identity domain.ChatCredentialInfo
					if e != nil || json.Unmarshal(raw, &identity) != nil || identity.Revoked || identity.ChatScope != domain.ChatScopeCenter && identity.ChatScope != domain.ChatScopeNode {
						fail(-32000, "Authenticated chat scope unavailable")
					} else {
						reply["result"] = map[string]any{"tools": chatToolsForScope(identity.ChatScope)}
					}
				}
			case "tools/call":
				if !initialized {
					fail(-32000, "Initialize first")
					break
				}
				var call struct {
					Name      string                     `json:"name"`
					Arguments json.RawMessage            `json:"arguments"`
					Meta      map[string]json.RawMessage `json:"_meta,omitempty"`
				}
				if bootstrapStrict(r.Params, &call) != nil {
					fail(-32602, "Invalid parameters")
					break
				}
				if len(call.Arguments) == 0 {
					call.Arguments = json.RawMessage(`{}`)
				}
				value, e := s.call(ctx, call.Name, call.Arguments)
				text := string(value)
				if e != nil {
					text = "Chat request rejected; refresh versions, check input and credential, or contact the Owner."
				}
				reply["result"] = map[string]any{"content": []any{map[string]string{"type": "text", "text": text}}, "isError": e != nil}
			default:
				fail(-32601, "Method not found")
			}
		}
		if e := enc.Encode(reply); e != nil {
			return e
		}
	}
	return scan.Err()
}
func (s Chat) call(ctx context.Context, name string, raw json.RawMessage) (json.RawMessage, error) {
	decode := func(v any) error { return jsoninput.Decode(raw, v, 32<<10) }
	var path, key string
	var input any
	switch name {
	case "get_project_overview":
		var v struct{}
		if e := decode(&v); e != nil {
			return nil, e
		}
		return s.API.Do(ctx, "GET", "/api/v1/chat/overview", "", nil)
	case "list_project_items":
		var v domain.ChatListQuery
		if e := decode(&v); e != nil {
			return nil, e
		}
		if e := domain.ValidateChatList(v); e != nil {
			return nil, e
		}
		path = "items"
		input = v
	case "get_project_item":
		var v domain.ChatItemQuery
		if e := decode(&v); e != nil {
			return nil, e
		}
		if e := domain.ValidateChatItem(v); e != nil {
			return nil, e
		}
		path = "item"
		input = v
	case "get_consultation":
		var v domain.ChatRequestQuery
		if e := decode(&v); e != nil {
			return nil, e
		}
		if !domain.ChatIdentifier.MatchString(v.ID) {
			return nil, errors.New("invalid identifier")
		}
		path = "consultation"
		input = v
	case "submit_requirement":
		var v struct {
			domain.ChatRequirementInput
			Key string `json:"idempotency_key"`
		}
		if e := decode(&v); e != nil {
			return nil, e
		}
		if e := domain.ValidateChatRequirement(v.ChatRequirementInput); e != nil {
			return nil, e
		}
		path = "requirements"
		key = v.Key
		input = v.ChatRequirementInput
	case "consult_leader", "propose_change":
		var v struct {
			domain.ConversationInput
			Key string `json:"idempotency_key"`
		}
		if e := decode(&v); e != nil {
			return nil, e
		}
		if e := domain.ValidateConversation(v.ConversationInput); e != nil {
			return nil, e
		}
		if e := jsoninput.Required(raw, "expected_goal_version", "original_text", "source", "idempotency_key"); e != nil {
			return nil, e
		}
		path = "consultations"
		if name == "propose_change" {
			path = "proposals"
		}
		key = v.Key
		input = v.ConversationInput
	case "cancel_consultation":
		var v struct {
			domain.ChatCancelRequest
			Key string `json:"idempotency_key"`
		}
		if e := decode(&v); e != nil {
			return nil, e
		}
		if !domain.ChatIdentifier.MatchString(v.ID) || v.ExpectedVersion < 1 {
			return nil, errors.New("invalid cancellation")
		}
		path = "cancel"
		key = v.Key
		input = v.ChatCancelRequest
	default:
		return nil, errors.New("unknown tool")
	}
	// Validate closed query enums/identifiers before constructing the fixed call;
	// the application repeats all domain validation under authentication.
	if (path == "requirements" || path == "consultations" || path == "proposals" || path == "cancel") && (key == "" || len(key) > 200) {
		return nil, errors.New("idempotency key required")
	}
	return s.API.Do(ctx, "POST", "/api/v1/chat/"+path, key, input)
}
func chatTools() []any {
	str := func(max int) any { return map[string]any{"type": "string", "maxLength": max} }
	kind := map[string]any{"type": "string", "enum": []string{"requirements", "tasks", "blocks", "agents", "runs", "consultations", "proposals"}}
	source := map[string]any{"type": "object", "properties": map[string]any{"client_id": str(128), "session_id": str(128), "message_id": str(128)}, "required": []string{"session_id", "message_id"}, "additionalProperties": false}
	tools := []any{
		tool("get_project_overview", "Read redacted project and goal facts. Does not invoke Leader.", map[string]any{}, nil),
		tool("list_project_items", "Read a bounded page of central records. Cursor is the last item ID.", map[string]any{"kind": kind, "cursor": str(128), "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 20}}, []string{"kind", "limit"}),
		tool("get_project_item", "Read one redacted central item with version and query time.", map[string]any{"kind": kind, "id": str(128)}, []string{"kind", "id"}),
		tool("get_consultation", "Read durable consultation or proposal status and advisory reply.", map[string]any{"id": str(128)}, []string{"id"}),
		tool("submit_requirement", "Explicitly submit a user requirement into the existing pool. Original text is retained; no approval or goal rewrite.", map[string]any{"title": str(512), "original_text": str(16384), "assistant_summary": str(4096), "source": source, "idempotency_key": str(200)}, []string{"title", "original_text", "source", "idempotency_key"}),
	}
	for _, name := range []string{"consult_leader", "propose_change"} {
		tools = append(tools, tool(name, "Queue an asynchronous advisory request. Relay is never Owner approval. Use expected_goal_version 0 when absent; adopt through Owner flows.", map[string]any{"expected_goal_version": map[string]any{"type": "integer", "minimum": 0}, "original_text": str(16384), "assistant_summary": str(4096), "source": source, "reply_to": str(128), "idempotency_key": str(200)}, []string{"expected_goal_version", "original_text", "source", "idempotency_key"}))
	}
	return append(tools, tool("cancel_consultation", "Request cancellation of your own consultation or proposal; wait for central stop evidence.", map[string]any{"id": str(128), "expected_version": map[string]any{"type": "integer", "minimum": 1}, "idempotency_key": str(200)}, []string{"id", "expected_version", "idempotency_key"}))
}

func chatToolsForScope(scope string) []any {
	tools := chatTools()
	if scope == domain.ChatScopeCenter {
		return tools
	}
	out := []any{}
	for _, t := range tools {
		v := t.(map[string]any)
		name, _ := v["name"].(string)
		if name == "get_consultation" || name == "consult_leader" || name == "propose_change" || name == "cancel_consultation" {
			continue
		}
		if name == "list_project_items" || name == "get_project_item" {
			props := v["inputSchema"].(map[string]any)["properties"].(map[string]any)
			props["kind"] = map[string]any{"type": "string", "enum": []string{"requirements", "agents", "runs"}}
		}
		out = append(out, v)
	}
	return out
}
