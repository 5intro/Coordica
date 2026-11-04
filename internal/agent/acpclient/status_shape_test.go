package acpclient

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func statusShape(raw []byte) (UpdateFact, error) {
	fields, err := object(raw)
	if err != nil {
		return UpdateFact{}, err
	}
	return updateFact(fields)
}

func assertStatusShape(t *testing.T, update any, valid bool) {
	t.Helper()
	raw, err := json.Marshal(update)
	if err != nil {
		t.Fatal(err)
	}
	fact, err := statusShape(raw)
	if valid {
		if err != nil || fact.Kind == "" || fact.TextBytes != 0 || fact.ToolCallID != "" || fact.State != "" || fact.ConfigVersion != 0 {
			t.Fatal("valid status did not remain kind-only metadata", fact, err)
		}
	} else if !errors.Is(err, ErrProtocol) {
		t.Fatal("invalid status shape accepted or misclassified", fact, err)
	}
}

func invalidStatusValues() []struct {
	name  string
	value any
} {
	return []struct {
		name  string
		value any
	}{
		{"missing", nil}, {"null", nil}, {"boolean", false}, {"number", 0}, {"array", []any{}}, {"object", map[string]any{}},
	}
}

func TestStatusPlanRequiredEntryFieldsAndClosedEnums(t *testing.T) {
	for _, field := range []string{"content", "priority", "status"} {
		for _, bad := range invalidStatusValues() {
			t.Run(field+"/"+bad.name, func(t *testing.T) {
				update := statusFixtureUpdates()[0]
				entry := update["entries"].([]any)[0].(map[string]any)
				if bad.name == "missing" {
					delete(entry, field)
				} else {
					entry[field] = bad.value
				}
				assertStatusShape(t, update, false)
			})
		}
	}
	for _, priority := range []string{"high", "medium", "low", "", "unknown"} {
		for _, status := range []string{"pending", "in_progress", "completed", "", "failed", "unknown"} {
			t.Run(priority+"/"+status, func(t *testing.T) {
				update := map[string]any{"sessionUpdate": "plan", "entries": []any{map[string]any{
					"content": "", "priority": priority, "status": status,
				}}}
				valid := priority != "" && priority != "unknown" && status != "" && status != "failed" && status != "unknown"
				assertStatusShape(t, update, valid)
			})
		}
	}
}

func TestStatusRequiredArraysAllowEmptyAndRepeatedDeclarations(t *testing.T) {
	for i, field := range []string{"entries", "availableCommands"} {
		for _, tc := range []struct {
			name  string
			value any
		}{
			{"missing", nil}, {"null", nil}, {"string", ""}, {"object", map[string]any{}},
			{"null-entry", []any{nil}}, {"array-entry", []any{[]any{}}}, {"missing-entry-fields", []any{map[string]any{}}},
		} {
			t.Run(field+"/"+tc.name, func(t *testing.T) {
				update := statusFixtureUpdates()[i]
				if tc.name == "missing" {
					delete(update, field)
				} else {
					update[field] = tc.value
				}
				assertStatusShape(t, update, false)
			})
		}
		update := statusFixtureUpdates()[i]
		entry := update[field].([]any)[0]
		update[field] = []any{entry, entry}
		assertStatusShape(t, update, true)
		update[field] = []any{}
		assertStatusShape(t, update, true)
	}
}

func TestStatusCommandRequiredFieldsAndOptionalInput(t *testing.T) {
	for _, field := range []string{"name", "description"} {
		for _, bad := range invalidStatusValues() {
			t.Run(field+"/"+bad.name, func(t *testing.T) {
				update := statusFixtureUpdates()[1]
				command := update["availableCommands"].([]any)[0].(map[string]any)
				if bad.name == "missing" {
					delete(command, field)
				} else {
					command[field] = bad.value
				}
				assertStatusShape(t, update, false)
			})
		}
	}
	for _, tc := range []struct {
		name  string
		input any
		valid bool
	}{
		{"absent", nil, true}, {"null", nil, true},
		{"empty", map[string]any{"hint": ""}, true},
		{"open-extension", map[string]any{"hint": "private", "type": "vendor-extension", "_meta": nil}, true},
		{"missing-hint", map[string]any{}, false}, {"null-hint", map[string]any{"hint": nil}, false},
		{"bad-hint", map[string]any{"hint": 0}, false}, {"array", []any{}, false}, {"string", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			command := map[string]any{"name": "", "description": "", "input": tc.input}
			if tc.name == "absent" {
				delete(command, "input")
			}
			assertStatusShape(t, map[string]any{"sessionUpdate": "available_commands_update", "availableCommands": []any{command}}, tc.valid)
		})
	}
}

func TestStatusModeIDsAreRequiredOpenStrings(t *testing.T) {
	for _, bad := range invalidStatusValues() {
		t.Run(bad.name, func(t *testing.T) {
			update := map[string]any{"sessionUpdate": "current_mode_update", "currentModeId": bad.value}
			if bad.name == "missing" {
				delete(update, "currentModeId")
			}
			assertStatusShape(t, update, false)
		})
	}
	for _, id := range []string{"", "unknown-native-id", "private permission\nmode"} {
		assertStatusShape(t, map[string]any{"sessionUpdate": "current_mode_update", "currentModeId": id}, true)
	}
}

func TestStatusExtensionsStayOpaqueAndMalformedObjectsStayInvalid(t *testing.T) {
	for _, update := range statusFixtureUpdates() {
		update["vendor"] = map[string]any{"anything": []any{nil, 5, "private"}}
		for _, meta := range []any{nil, map[string]any{"private": []any{1, nil, false}}, []any{}, 3} {
			update["_meta"] = meta
			_, isObject := meta.(map[string]any)
			assertStatusShape(t, update, meta == nil || isObject)
		}
	}
	for _, raw := range []string{
		`{"sessionUpdate":"plan","entries":[{"content":"x","priority":"high","status":"pending","_meta":[]} ]}`,
		`{"sessionUpdate":"available_commands_update","availableCommands":[{"name":"x","description":"","_meta":0}]}`,
		`{"sessionUpdate":"available_commands_update","availableCommands":[{"name":"x","description":"","input":{"hint":"","_meta":[]}}]}`,
		`{"sessionUpdate":"plan","entries":[],"entries":[]}`,
		`{"sessionUpdate":"plan","entries":[{"content":"x","priority":"high","status":"pending","status":"completed"}]}`,
		`{"sessionUpdate":"current_mode_update","currentModeId":"x","vendor":{"duplicate":1,"duplicate":2}}`,
		`{"sessionUpdate":"current_mode_update","currentModeId":"` + string([]byte{255}) + `"}`,
		`null`, `[]`, `{"sessionUpdate":null}`, `{"currentModeId":"x"}`,
	} {
		if _, err := statusShape([]byte(raw)); !errors.Is(err, ErrProtocol) {
			t.Fatal("malformed status or duplicate/invalid extension accepted", err)
		}
	}
	if _, err := statusShape([]byte(`{"sessionUpdate":"future_native_variant"}`)); !errors.Is(err, ErrUnsupportedUpdate) {
		t.Fatal("unknown variant was silently accepted", err)
	}
}

func TestStatusWholeFrameBoundAppliesBeforePayloadRetention(t *testing.T) {
	frame := func(size int) []byte {
		raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{
			"sessionId": "s", "update": map[string]any{"sessionUpdate": "current_mode_update", "currentModeId": strings.Repeat("x", size)},
		}})
		return raw
	}
	capture, err := newUpdateCapture()
	if err != nil {
		t.Fatal(err)
	}
	p := &Peer{sessionReady: true, sessionID: "s", updates: capture}
	size := maxFrameBytes - len(frame(0))
	if raw := frame(size); len(raw) != maxFrameBytes || p.receive(raw) != nil {
		t.Fatal("legal frame within bound was rejected")
	}
	if len(capture.facts) != 1 || capture.facts[0].Kind != "current_mode_update" || capture.bytes > 128 {
		t.Fatal("mode body entered retained metadata")
	}
	if err = p.receive(frame(size + 1)); !errors.Is(err, ErrProtocol) || len(capture.facts) != 1 {
		t.Fatal("oversize frame was accepted", err)
	}
}
