package mcp

import (
	"context"
	"encoding/json"
	"errors"

	"coordica/internal/centerhost"
)

// CenterLocal is constructed only by trusted --host-role center startup.
// No call arguments can change its host identity or fixed configuration.
type CenterLocal struct {
	Manager centerhost.Manager
}

func (c CenterLocal) Tools(context.Context) ([]any, error) {
	return []any{
		tool("initialize_center", "Initialize and request activation of the trusted fixed center. Registers the selected project paused and draft; never returns Owner credentials.", map[string]any{}, nil),
		tool("start_center", "Idempotently request activation of the saved center; preserves pause, automation, identities and permissions.", map[string]any{}, nil),
		tool("get_host_status", "Read local setup and matching authenticated center health. OS activation, Runner heartbeat and model discovery are distinct facts.", map[string]any{}, nil),
	}, nil
}
func (c CenterLocal) Owns(name string) bool {
	return name == "initialize_center" || name == "start_center" || name == "get_host_status"
}
func (c CenterLocal) Call(ctx context.Context, name string, args json.RawMessage) (string, bool) {
	var in struct{}
	if e := rpcObject(args, &in); e != nil {
		return "Fixed host operation takes no arguments.", true
	}
	var value any
	var e error
	switch name {
	case "initialize_center", "start_center":
		value, e = c.Manager.Initialize(ctx)
	case "get_host_status":
		value, e = c.Manager.Status(ctx)
	default:
		e = errors.New("unknown fixed center operation")
	}
	if e != nil {
		raw, _ := json.Marshal(map[string]any{"error": e.Error(), "result": value})
		return string(raw), true
	}
	raw, e := json.Marshal(value)
	return string(raw), e != nil
}
