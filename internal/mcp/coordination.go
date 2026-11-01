package mcp

import (
	"context"
	"encoding/json"
	"errors"

	"coordica/internal/domain"
	"coordica/internal/jsoninput"
)

func workTool(name string) bool {
	return name == "list_work_contexts" || name == "get_work_context" || name == "record_work_note"
}

func callWork(ctx context.Context, api Caller, name string, raw json.RawMessage) (json.RawMessage, error) {
	var input any
	var path, key string
	switch name {
	case "list_work_contexts":
		var in domain.WorkListQuery
		if err := jsoninput.Decode(raw, &in, domain.MaxWorkInputBytes); err != nil {
			return nil, err
		}
		if err := domain.ValidateWorkListQuery(in); err != nil {
			return nil, err
		}
		input, path = in, "contexts"
	case "get_work_context":
		var in domain.WorkContextQuery
		if err := jsoninput.Decode(raw, &in, domain.MaxWorkInputBytes); err != nil {
			return nil, err
		}
		if err := domain.ValidateWorkContextQuery(in); err != nil {
			return nil, err
		}
		input, path = in, "context"
	case "record_work_note":
		var in struct {
			domain.WorkNoteInput
			Key string `json:"idempotency_key"`
		}
		if err := jsoninput.Decode(raw, &in, domain.MaxWorkInputBytes); err != nil {
			return nil, err
		}
		if err := jsoninput.Required(raw, "anchor", "expected_stamp", "kind", "original_text", "references", "source", "idempotency_key"); err != nil {
			return nil, err
		}
		if err := domain.ValidateWorkNote(in.WorkNoteInput); err != nil {
			return nil, err
		}
		if in.Key == "" || len(in.Key) > 200 {
			return nil, errors.New("idempotency key required")
		}
		input, path, key = in.WorkNoteInput, "notes", in.Key
	default:
		return nil, errors.New("unknown work tool")
	}
	return api.Do(ctx, "POST", "/api/v1/chat/work/"+path, key, input)
}

func workTools() []any {
	str := func(max int) any { return map[string]any{"type": "string", "maxLength": max} }
	object := func(props map[string]any, required ...string) any {
		return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
	}
	anchorKind := map[string]any{"type": "string", "enum": []string{"project", "requirement", "task"}}
	anchor := object(map[string]any{"kind": anchorKind, "id": str(128)}, "kind", "id")
	integer := map[string]any{"type": "integer", "minimum": 0}
	ref := object(map[string]any{
		"kind": map[string]any{"type": "string", "enum": []string{
			"project", "requirement", "task", "run", "candidate", "decision", "consultation", "initialization", "goal",
		}},
		"id": str(128), "version": integer, "authorization_version": integer,
		"generation": integer, "revision": integer, "stamp": str(64),
	}, "kind", "id")
	refs := map[string]any{"type": "array", "maxItems": 16, "items": ref}
	limit := map[string]any{"type": "integer", "minimum": 1, "maximum": 100}
	source := object(map[string]any{"client_id": str(128), "session_id": str(128), "message_id": str(128)}, "session_id", "message_id")
	return []any{
		tool("list_work_contexts",
			"Find explicitly saved work visible to this credential across sessions. "+
				"Multiple matches require user selection; never invokes a model.",
			map[string]any{"cursor": str(2048), "limit": limit, "kind": anchorKind}, []string{"limit"}),
		tool("get_work_context",
			"Read current authorized facts, saved notes and referenced replies/questions. "+
				"Refresh references on every reconnect; cursors page history, not source updates. "+
				"Unsupported worker delivery remains unavailable.",
			map[string]any{
				"anchor": anchor, "cursor": str(2048), "reference_cursor": str(2048), "references": refs, "limit": limit,
			}, []string{"anchor", "limit"}),
		tool("record_work_note",
			"Explicitly save original user discussion or pinned references. Receipt means recorded only: "+
				"no delivery, requirement, approval, contract change or execution. "+
				"Copy current stamps; keep source/key for retry.",
			map[string]any{
				"anchor": anchor, "expected_stamp": str(64),
				"kind":          map[string]any{"type": "string", "enum": []string{"note", "reference"}},
				"original_text": str(16384), "assistant_summary": str(4096), "references": refs,
				"corrects": str(128), "source": source, "idempotency_key": str(200),
			}, []string{"anchor", "expected_stamp", "kind", "original_text", "references", "source", "idempotency_key"}),
	}
}
