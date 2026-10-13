// Package mcp exposes one stdio protocol with startup-bound interaction,
// compatibility and Run identities. It has no database access or Owner authority.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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

func (s Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	return serve(ctx, in, out, false, s)
}
func (s Server) Tools(context.Context) ([]any, error) {
	return []any{tool("get_context", "Read this Run's task, authorization and fixed candidate.", map[string]any{}, nil),
		tool("report_block", "Stop and report a required human decision.", map[string]any{"reason": map[string]string{"type": "string"}, "idempotency_key": map[string]string{"type": "string"}}, []string{"reason", "idempotency_key"}),
		tool("request_scope", "Request ordinary additional paths; stops work before assessment.", map[string]any{"paths": map[string]any{"type": "array", "items": map[string]string{"type": "string"}}, "reason": map[string]string{"type": "string"}, "idempotency_key": map[string]string{"type": "string"}}, []string{"reason", "idempotency_key"})}, nil
}
func (s Server) Owns(name string) bool {
	return name == "get_context" || name == "report_block" || name == "request_scope"
}
func (s Server) Call(ctx context.Context, name string, arguments json.RawMessage) (string, bool) {
	var result json.RawMessage
	var err error
	switch name {
	case "get_context":
		var args struct{}
		err = rpcObject(arguments, &args)
		if err == nil {
			result, err = s.API.Do(ctx, "GET", "/api/v1/runs/"+s.RunID+"/context", "", nil)
		}
	case "report_block", "request_scope":
		var args struct {
			Paths  []string `json:"paths,omitempty"`
			Reason string   `json:"reason"`
			Key    string   `json:"idempotency_key"`
		}
		err = rpcObject(arguments, &args)
		if err == nil && (strings.TrimSpace(args.Reason) == "" || args.Key == "") {
			err = fmt.Errorf("reason and idempotency_key are required")
		}
		if err == nil {
			if name == "request_scope" {
				result, err = s.API.Do(ctx, "POST", "/api/v1/runs/"+s.RunID+"/request_scope", args.Key, map[string]any{"generation": s.Generation, "reason": args.Reason, "paths": args.Paths})
			} else {
				result, err = s.API.Do(ctx, "POST", "/api/v1/runs/"+s.RunID+"/block", args.Key, map[string]any{"generation": s.Generation, "reason": args.Reason})
			}
		}
	default:
		return "Unknown tool", true
	}
	if err != nil {
		return err.Error(), true
	}
	return string(result), false
}
