package mcp

import (
	"bufio"
	"bytes"
	"context"
	"coordica/internal/domain"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// toolProvider is owned by the stdio dispatcher. Owns defines a closed routing
// table; discovery never substitutes for per-call authorization in a provider.
type toolProvider interface {
	Tools(context.Context) ([]any, error)
	Owns(string) bool
	Call(context.Context, string, json.RawMessage) (string, bool)
}

func hasTool(tools []any, name string) bool {
	for _, t := range tools {
		if t.(map[string]any)["name"] == name {
			return true
		}
	}
	return false
}

func serve(ctx context.Context, in io.Reader, out io.Writer, refresh bool, providers ...toolProvider) error {
	scan := bufio.NewScanner(in)
	scan.Buffer(make([]byte, 4096), 1<<20)
	enc := json.NewEncoder(out)
	initialized := false
	var lastTools string
	discover := func() ([]any, error) {
		tools := []any{}
		for _, p := range providers {
			found, err := p.Tools(ctx)
			if err != nil {
				return nil, err
			}
			tools = append(tools, found...)
		}
		return tools, nil
	}
	for scan.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var r request
		reply := map[string]any{"jsonrpc": "2.0", "id": json.RawMessage("null")}
		fail := func(code int, msg string) { reply["error"] = map[string]any{"code": code, "message": msg} }
		switch {
		case !json.Valid(scan.Bytes()):
			fail(-32700, "Parse error")
		case rpcObject(scan.Bytes(), &r) != nil || r.JSONRPC != "2.0" || r.Method == "" || len(r.ID) > 0 && !validRPCID(r.ID):
			fail(-32600, "Invalid request")
		case len(r.ID) == 0:
			continue
		default:
			reply["id"] = r.ID
			switch r.Method {
			case "initialize":
				var p struct {
					ProtocolVersion string                     `json:"protocolVersion"`
					Capabilities    map[string]json.RawMessage `json:"capabilities"`
					ClientInfo      map[string]json.RawMessage `json:"clientInfo,omitempty"`
					Meta            map[string]json.RawMessage `json:"_meta,omitempty"`
				}
				if rpcObject(r.Params, &p) != nil || strings.TrimSpace(p.ProtocolVersion) == "" || p.ClientInfo != nil && !validClientInfo(p.ClientInfo) {
					fail(-32602, "Invalid initialization parameters")
					break
				}
				initialized = true
				capabilities := map[string]any{}
				if refresh {
					capabilities["listChanged"] = true
				}
				reply["result"] = map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{"tools": capabilities}, "serverInfo": map[string]string{"name": "coordica", "version": domain.SourceVersion}}
			case "ping", "tools/list":
				if len(r.Params) > 0 {
					var p struct {
						Meta map[string]json.RawMessage `json:"_meta,omitempty"`
					}
					if rpcObject(r.Params, &p) != nil {
						fail(-32602, "Invalid parameters")
						break
					}
				}
				if r.Method == "ping" {
					reply["result"] = map[string]any{}
				} else if !initialized {
					fail(-32000, "Initialize first")
				} else {
					tools, err := discover()
					if err != nil {
						fail(-32000, "Authenticated chat scope unavailable")
					} else {
						reply["result"] = map[string]any{"tools": tools}
						raw, _ := json.Marshal(tools)
						lastTools = string(raw)
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
				if rpcObject(r.Params, &call) != nil || call.Name == "" {
					fail(-32602, "Invalid parameters")
					break
				}
				if len(call.Arguments) == 0 {
					call.Arguments = json.RawMessage(`{}`)
				}
				var owner toolProvider
				for _, p := range providers {
					if p.Owns(call.Name) {
						owner = p
						break
					}
				}
				if owner == nil {
					fail(-32602, "Unknown tool")
					break
				}
				text, isError := owner.Call(ctx, call.Name, call.Arguments)
				reply["result"] = map[string]any{"content": []any{map[string]string{"type": "text", "text": text}}, "isError": isError}
			default:
				fail(-32601, "Method not found")
			}
		}
		if err := enc.Encode(reply); err != nil {
			return err
		}
		// Observe readiness on client activity, without a polling timer. Clients
		// without notification support can explicitly rediscover with tools/list.
		if refresh && initialized && r.Method != "initialize" && r.Method != "tools/list" {
			tools, err := discover()
			if err == nil {
				raw, _ := json.Marshal(tools)
				if lastTools != "" && lastTools != string(raw) {
					if err := enc.Encode(map[string]any{"jsonrpc": "2.0", "method": "notifications/tools/list_changed"}); err != nil {
						return err
					}
				}
				lastTools = string(raw)
			}
		}
	}
	return scan.Err()
}

func tool(name, desc string, props map[string]any, required []string) any {
	if required == nil {
		required = []string{}
	}
	return map[string]any{"name": name, "description": desc, "inputSchema": map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}}
}
func strict(raw []byte, out any) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return fmt.Errorf("parameters must be a JSON object")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if e := d.Decode(out); e != nil {
		return e
	}
	if d.Decode(new(any)) != io.EOF {
		return fmt.Errorf("trailing input")
	}
	return nil
}

func validRPCID(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	var name string
	if json.Unmarshal(raw, &name) == nil && string(raw) != "null" {
		return true
	}
	_, e := strconv.ParseInt(string(raw), 10, 64)
	return e == nil
}

// Client implementation metadata is descriptive, not an authority source.
// Keep name/version well typed while accepting standard and future metadata.
func validClientInfo(info map[string]json.RawMessage) bool {
	for _, field := range []string{"name", "version"} {
		var value string
		if json.Unmarshal(info[field], &value) != nil || strings.TrimSpace(value) == "" {
			return false
		}
	}
	return true
}

// Keep the existing strict decoder, adding object and duplicate-key checks.
// In particular, JSON null must not turn into an empty save-tool argument.
func rpcObject(raw []byte, value any) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return fmt.Errorf("parameters must be a JSON object")
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	if err := uniqueJSON(decoder); err != nil {
		return err
	}
	return strict(trimmed, value)
}

func uniqueJSON(decoder *json.Decoder) error {
	first, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, nested := first.(json.Delim)
	if !nested {
		return nil
	}
	keys := map[string]bool{}
	for decoder.More() {
		if delimiter == '{' {
			token, err := decoder.Token()
			if err != nil {
				return err
			}
			key := token.(string)
			if keys[key] {
				return fmt.Errorf("duplicate JSON field %q", key)
			}
			keys[key] = true
		}
		if err := uniqueJSON(decoder); err != nil {
			return err
		}
	}
	_, err = decoder.Token()
	return err
}
