package mcp

import (
	"context"
	"coordica/internal/domain"
	"coordica/internal/jsoninput"
	"encoding/json"
	"errors"
)

func callPreparation(ctx context.Context, api Caller, name string, raw json.RawMessage) (json.RawMessage, error) {
	decode := func(v any) error { return jsoninput.Decode(raw, v, 32<<10) }
	var path, key string
	var input any
	switch name {
	case "get_project_initialization":
		var v struct{}
		if e := decode(&v); e != nil {
			return nil, e
		}
		return api.Do(ctx, "GET", "/api/v1/chat/initialization", "", nil)
	case "open_project":
		var v struct {
			domain.InitializationRequest
			Key string `json:"idempotency_key"`
		}
		if e := decode(&v); e != nil {
			return nil, e
		}
		if e := jsoninput.Required(raw, "expected_version", "kind", "idempotency_key"); e != nil {
			return nil, e
		}
		path = "initialization/analyze"
		key = v.Key
		input = v.InitializationRequest
	case "answer_project_initialization":
		var v struct {
			domain.InitializationAnswer
			Key string `json:"idempotency_key"`
		}
		if e := decode(&v); e != nil {
			return nil, e
		}
		if e := jsoninput.Required(raw, "expected_version", "question_id", "original_text", "source", "idempotency_key"); e != nil {
			return nil, e
		}
		path = "initialization/answer"
		key = v.Key
		input = v.InitializationAnswer
	case "read_leader_activity":
		var query domain.LeaderActivityQuery
		if err := decode(&query); err != nil {
			return nil, err
		}
		if err := domain.ValidateLeaderActivityQuery(query); err != nil {
			return nil, err
		}
		path, input = "leader/activity", query
	default:
		return nil, errors.New("unsupported preparation operation")
	}
	if path != "leader/activity" && (key == "" || len(key) > 200) {
		return nil, errors.New("idempotency key required")
	}
	return api.Do(ctx, "POST", "/api/v1/chat/"+path, key, input)
}

func leaderActivityTool() any {
	return tool("read_leader_activity", "Read bounded public Leader activity; display is not authority or completion evidence.", map[string]any{
		"after": map[string]any{"type": "integer", "minimum": 0, "maximum": 9007199254740991},
		"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
		"kind":  map[string]any{"type": "string", "enum": []string{"", "planning", "initialization"}},
		"id":    map[string]any{"type": "string", "maxLength": 128}, "generation": map[string]any{"type": "integer", "minimum": 0},
	}, []string{"limit"})
}
