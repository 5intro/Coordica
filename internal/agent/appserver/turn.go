package appserver

import (
	"context"
	"encoding/json"
	"unicode/utf8"

	"coordica/internal/jsoninput"
)

// Turn pins a native thread/turn pair. The originating handle is immutable;
// durable Run ownership, single-writer policy and generation fencing remain
// application responsibilities. A handle does not prove a Turn is still active.
type Turn struct {
	thread Thread
	id     string
	status string
}

func (t Turn) ID() string            { return t.id }
func (t Turn) ThreadID() string      { return t.thread.id }
func (t Turn) InitialStatus() string { return t.status }

// StartText is the first explicit text-only operation. Native tool/reasoning
// execution remains in Codex. Other input types require a separate typed slice;
// callers cannot smuggle config or tool definitions through this message.
func (t Thread) StartText(ctx context.Context, text, effort string) (Turn, error) {
	if !t.valid() {
		return Turn{}, ErrInvalidTarget
	}
	if !validText(text) || !validNativeID(effort) {
		return Turn{}, ErrProtocol
	}
	var result struct {
		Turn json.RawMessage `json:"turn"`
	}
	err := t.peer.Call(ctx, "turn/start", map[string]any{"threadId": t.id, "input": textInput(text), "effort": effort}, &result)
	if err != nil {
		return Turn{}, err
	}
	var value struct {
		ID     string            `json:"id"`
		Status string            `json:"status"`
		Items  []json.RawMessage `json:"items"`
	}
	if jsoninput.Required(result.Turn, "id", "status", "items") != nil || json.Unmarshal(result.Turn, &value) != nil ||
		!validNativeID(value.ID) || !validTurnStatus(value.Status) {
		return Turn{}, t.peer.invalidResult()
	}
	return Turn{thread: t, id: value.ID, status: value.Status}, nil
}

// SteerText targets this exact native Turn. A native stale-target rejection is
// returned unchanged; it never becomes a queued message or a new Turn.
func (t Turn) SteerText(ctx context.Context, text string) error {
	if !t.valid() {
		return ErrInvalidTarget
	}
	if !validText(text) {
		return ErrProtocol
	}
	var result struct {
		TurnID string `json:"turnId"`
	}
	err := t.thread.peer.Call(ctx, "turn/steer", map[string]any{
		"threadId": t.thread.id, "expectedTurnId": t.id, "input": textInput(text)}, &result)
	if err == nil && result.TurnID != t.id {
		return t.thread.peer.invalidResult()
	}
	return err
}

// Interrupt acknowledges only the native RPC. Turn completion and background
// work/process stop evidence arrive separately; this does not release a Session.
func (t Turn) Interrupt(ctx context.Context) error {
	if !t.valid() {
		return ErrInvalidTarget
	}
	var result map[string]json.RawMessage
	err := t.thread.peer.Call(ctx, "turn/interrupt", map[string]string{"threadId": t.thread.id, "turnId": t.id}, &result)
	if err == nil && result == nil {
		return t.thread.peer.invalidResult()
	}
	return err
}

func (t Turn) valid() bool { return t.thread.valid() && validNativeID(t.id) }

func validText(text string) bool {
	return text != "" && len(text) <= maxFrameBytes/2 && utf8.ValidString(text)
}

func textInput(text string) []map[string]string {
	return []map[string]string{{"type": "text", "text": text}}
}

func validTurnStatus(status string) bool {
	return status == "inProgress" || status == "completed" || status == "interrupted" || status == "failed"
}
