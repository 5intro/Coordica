// Package mcp exposes a Run-scoped stdio protocol. It has no database access
// and cannot approve work, change gates or publish branches.
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

type Caller interface {
	Do(context.Context, string, string, string, any) (json.RawMessage, error)
}
type Server struct {
	API        Caller
	RunID      string
	Generation int64
}
type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

func (s Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	scan := bufio.NewScanner(in)
	scan.Buffer(make([]byte, 4096), 1<<20)
	enc := json.NewEncoder(out)
	initialized := false
	for scan.Scan() {
		var r request
		reply := map[string]any{"jsonrpc": "2.0", "id": json.RawMessage("null")}
		fail := func(code int, msg string) { reply["error"] = map[string]any{"code": code, "message": msg} }
		if json.Unmarshal(scan.Bytes(), &r) != nil {
			fail(-32700, "Parse error")
		} else if r.JSONRPC != "2.0" || r.Method == "" || len(r.ID) > 0 && !validRPCID(r.ID) {
			fail(-32600, "Invalid request")
		} else {
			if len(r.ID) == 0 {
				continue
			}
			reply["id"] = r.ID
			switch r.Method {
			case "initialize":
				initialized = true
				reply["result"] = map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]string{"name": "coordica-run", "version": "0.1.0"}}
			case "ping":
				reply["result"] = map[string]any{}
			case "tools/list":
				if !initialized {
					fail(-32000, "Initialize first")
					break
				}
				reply["result"] = map[string]any{"tools": []any{
					tool("get_context", "Read this Run's task, authorization and fixed candidate.", map[string]any{}, nil),
					tool("report_block", "Stop and report a required human decision.", map[string]any{"reason": map[string]string{"type": "string"}, "idempotency_key": map[string]string{"type": "string"}}, []string{"reason", "idempotency_key"}),
					tool("request_scope", "Request ordinary additional paths; stops work before assessment.", map[string]any{"paths": map[string]any{"type": "array", "items": map[string]string{"type": "string"}}, "reason": map[string]string{"type": "string"}, "idempotency_key": map[string]string{"type": "string"}}, []string{"reason", "idempotency_key"}),
				}}
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
				if strict(r.Params, &call) != nil {
					fail(-32602, "Invalid parameters")
					break
				}
				if len(call.Arguments) == 0 {
					call.Arguments = json.RawMessage(`{}`)
				}
				var result json.RawMessage
				var err error
				switch call.Name {
				case "get_context":
					var args struct{}
					err = strict(call.Arguments, &args)
					if err == nil {
						result, err = s.API.Do(ctx, "GET", "/api/v1/runs/"+s.RunID+"/context", "", nil)
					}
				case "report_block", "request_scope":
					var args struct {
						Paths  []string `json:"paths,omitempty"`
						Reason string   `json:"reason"`
						Key    string   `json:"idempotency_key"`
					}
					err = strict(call.Arguments, &args)
					if err == nil && (strings.TrimSpace(args.Reason) == "" || args.Key == "") {
						err = fmt.Errorf("reason and idempotency_key are required")
					}
					if err == nil {
						if call.Name == "request_scope" {
							result, err = s.API.Do(ctx, "POST", "/api/v1/runs/"+s.RunID+"/request_scope", args.Key, map[string]any{"generation": s.Generation, "reason": args.Reason, "paths": args.Paths})
						} else {
							result, err = s.API.Do(ctx, "POST", "/api/v1/runs/"+s.RunID+"/block", args.Key, map[string]any{"generation": s.Generation, "reason": args.Reason})
						}
					}
				default:
					fail(-32602, "Unknown tool")
					break
				}
				if reply["error"] == nil {
					text := string(result)
					if err != nil {
						text = err.Error()
					}
					reply["result"] = map[string]any{"content": []any{map[string]string{"type": "text", "text": text}}, "isError": err != nil}
				}
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
