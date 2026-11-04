package acpclient

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

const permissionParserParams = `{"sessionId":"s","toolCall":{"toolCallId":"t"},"options":[]}`

func permissionParserFields(params string) map[string]json.RawMessage {
	return map[string]json.RawMessage{
		"jsonrpc": json.RawMessage(`"2.0"`),
		"method":  json.RawMessage(`"session/request_permission"`),
		"id":      json.RawMessage(`"permission-private"`),
		"params":  json.RawMessage(params),
	}
}

func TestPermissionIDTypedCanonicalKeys(t *testing.T) {
	cases := []struct {
		raw, key string
	}{
		{`null`, "null"}, {`""`, "s:"}, {`"7"`, "s:7"}, {`7`, "n:7"},
		{`0`, "n:0"}, {`-0`, "n:0"}, {`-7`, "n:-7"},
		{`-9223372036854775808`, "n:-9223372036854775808"},
		{`9223372036854775807`, "n:9223372036854775807"},
		{`"opaque \t\n\u0000"`, "s:opaque \t\n\x00"},
		{`"\u0037"`, "s:7"}, {` "null" `, "s:null"},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			key, err := permissionID(json.RawMessage(tc.raw))
			if err != nil || key != tc.key {
				t.Fatalf("id key=%q error=%v, want %q", key, err, tc.key)
			}
		})
	}
}

func TestPermissionIDInvalidAndUnsupportedSubset(t *testing.T) {
	cases := []struct {
		name, raw string
		want      error
	}{
		{"missing", "", ErrProtocol}, {"bool", "true", ErrProtocol},
		{"object", `{}`, ErrProtocol}, {"array", `[]`, ErrProtocol},
		{"positive-overflow", `9223372036854775808`, ErrProtocol},
		{"negative-overflow", `-9223372036854775809`, ErrProtocol},
		{"invalid-number", `01`, ErrProtocol}, {"plus-sign", `+1`, ErrProtocol},
		{"fraction-spelling", `1.0`, ErrUnsupportedPermission},
		{"fraction-value", `1.5`, ErrUnsupportedPermission},
		{"exponent-spelling", `1e0`, ErrUnsupportedPermission},
		{"upper-exponent", `1E0`, ErrUnsupportedPermission},
		{"long-string", `"` + strings.Repeat("x", 513) + `"`, ErrUnsupportedPermission},
		{"invalid-utf8", string([]byte{'"', 0xff, '"'}), ErrProtocol},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := permissionID(json.RawMessage(tc.raw)); !errors.Is(err, tc.want) {
				t.Fatalf("error=%v, want %v", err, tc.want)
			}
		})
	}
	encoded, _ := json.Marshal(strings.Repeat("界", 170) + "ab")
	if _, err := permissionID(encoded); err != nil {
		t.Fatalf("512 decoded bytes rejected: %v", err)
	}
}

func TestPermissionParserEnvelope(t *testing.T) {
	cases := []struct {
		name, field, raw string
		remove           bool
	}{
		{"missing-version", "jsonrpc", "", true},
		{"null-version", "jsonrpc", "null", false},
		{"wrong-version", "jsonrpc", `"1.0"`, false},
		{"wrong-version-type", "jsonrpc", "2", false},
		{"missing-method", "method", "", true},
		{"wrong-method", "method", `"session/update"`, false},
		{"null-method", "method", "null", false},
		{"missing-id-not-notification", "id", "", true},
		{"missing-params", "params", "", true},
		{"null-params", "params", "null", false},
		{"wrong-params-type", "params", "[]", false},
		{"extra-top-level-field", "_meta", "{}", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fields := permissionParserFields(permissionParserParams)
			if tc.remove {
				delete(fields, tc.field)
			} else {
				fields[tc.field] = json.RawMessage(tc.raw)
			}
			if _, err := parsePermission(fields); !errors.Is(err, ErrProtocol) {
				t.Fatalf("error=%v, want protocol error", err)
			}
		})
	}
}

func TestPermissionParserRequiredParamsAndOptions(t *testing.T) {
	cases := []struct {
		name, params string
		want         error
	}{
		{"missing-session", `{"toolCall":{"toolCallId":"t"},"options":[]}`, ErrProtocol},
		{"null-session", `{"sessionId":null,"toolCall":{"toolCallId":"t"},"options":[]}`, ErrProtocol},
		{"wrong-session-type", `{"sessionId":1,"toolCall":{"toolCallId":"t"},"options":[]}`, ErrProtocol},
		{"missing-tool", `{"sessionId":"s","options":[]}`, ErrProtocol},
		{"null-tool", `{"sessionId":"s","toolCall":null,"options":[]}`, ErrProtocol},
		{"missing-options", `{"sessionId":"s","toolCall":{"toolCallId":"t"}}`, ErrProtocol},
		{"null-options", `{"sessionId":"s","toolCall":{"toolCallId":"t"},"options":null}`, ErrProtocol},
		{"wrong-options-type", `{"sessionId":"s","toolCall":{"toolCallId":"t"},"options":{}}`, ErrProtocol},
		{"null-option", `{"sessionId":"s","toolCall":{"toolCallId":"t"},"options":[null]}`, ErrProtocol},
		{"missing-option-id", `{"sessionId":"s","toolCall":{"toolCallId":"t"},"options":[{"name":"n","kind":"allow_once"}]}`, ErrProtocol},
		{"null-option-id", `{"sessionId":"s","toolCall":{"toolCallId":"t"},"options":[{"optionId":null,"name":"n","kind":"allow_once"}]}`, ErrProtocol},
		{"wrong-option-id", `{"sessionId":"s","toolCall":{"toolCallId":"t"},"options":[{"optionId":3,"name":"n","kind":"allow_once"}]}`, ErrProtocol},
		{"missing-name", `{"sessionId":"s","toolCall":{"toolCallId":"t"},"options":[{"optionId":"o","kind":"allow_once"}]}`, ErrProtocol},
		{"null-name", `{"sessionId":"s","toolCall":{"toolCallId":"t"},"options":[{"optionId":"o","name":null,"kind":"allow_once"}]}`, ErrProtocol},
		{"missing-kind", `{"sessionId":"s","toolCall":{"toolCallId":"t"},"options":[{"optionId":"o","name":"n"}]}`, ErrProtocol},
		{"null-kind", `{"sessionId":"s","toolCall":{"toolCallId":"t"},"options":[{"optionId":"o","name":"n","kind":null}]}`, ErrProtocol},
		{"unknown-kind", `{"sessionId":"s","toolCall":{"toolCallId":"t"},"options":[{"optionId":"o","name":"n","kind":"allow"}]}`, ErrProtocol},
		{"duplicate-option-id", `{"sessionId":"s","toolCall":{"toolCallId":"t"},"options":[` +
			`{"optionId":"","name":"","kind":"allow_once"},{"optionId":"","name":"","kind":"reject_once"}]}`, ErrUnsupportedPermission},
		{"ambiguous-decoded-option-id", `{"sessionId":"s","toolCall":{"toolCallId":"t"},"options":[` +
			`{"optionId":"\ud800","name":"","kind":"allow_once"},{"optionId":"\ufffd","name":"","kind":"reject_once"}]}`, ErrUnsupportedPermission},
		{"invalid-meta", `{"sessionId":"s","toolCall":{"toolCallId":"t"},"options":[],"_meta":[]}`, ErrProtocol},
		{"duplicate-param-key", `{"sessionId":"s","sessionId":"s","toolCall":{"toolCallId":"t"},"options":[]}`, ErrProtocol},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parsePermission(permissionParserFields(tc.params)); !errors.Is(err, tc.want) {
				t.Fatalf("error=%v, want %v", err, tc.want)
			}
		})
	}
}

func TestPermissionParserPreservesNativeChoicesAndExtensions(t *testing.T) {
	params := `{"sessionId":"","toolCall":{"toolCallId":"","_meta":null},"options":[` +
		`{"optionId":"","name":"","kind":"allow_once","_meta":{"private":"retained only natively"}},` +
		`{"optionId":"a","name":"Remember","kind":"allow_always"},` +
		`{"optionId":"r","name":"Reject","kind":"reject_once"},` +
		`{"optionId":"R","name":"Always reject","kind":"reject_always"}],"_meta":{"opaque":true},"extension":[1]}`
	parsed, err := parsePermission(permissionParserFields(params))
	if err != nil || len(parsed.options) != 4 || parsed.options[0].ID != "" || parsed.options[0].Name != "" || parsed.sessionID != "" {
		t.Fatalf("schema-permitted opaque choices rejected: %+v, %v", parsed, err)
	}
	if parsed.options[1].Kind != "allow_always" || parsed.options[3].Kind != "reject_always" {
		t.Fatal("native kinds were mapped or lost")
	}
	empty, err := parsePermission(permissionParserFields(permissionParserParams))
	if err != nil || len(empty.options) != 0 {
		t.Fatal("empty options should be valid without an invented choice", err)
	}
}

func TestPermissionParserToolCallShallowSubset(t *testing.T) {
	valid := []string{
		`{"toolCallId":"t"}`,
		`{"toolCallId":"t","title":null,"status":null,"kind":null,"content":null,"locations":null}`,
		`{"toolCallId":"t","title":"","status":"pending","kind":"execute","content":[],"locations":[]}`,
		`{"toolCallId":"t","rawInput":[1,null],"rawOutput":false,"_meta":{"private":"text"},"extension":3}`,
		`{"toolCallId":"t","content":[null,3],"locations":[false]}`,
	}
	for _, raw := range valid {
		if err := permissionToolCall(json.RawMessage(raw)); err != nil {
			t.Fatalf("accepted shallow subset rejected: %s: %v", raw, err)
		}
	}
	invalid := []string{
		`null`, `[]`, `{}`, `{"toolCallId":null}`, `{"toolCallId":1}`,
		`{"toolCallId":"t","title":false}`, `{"toolCallId":"t","status":1}`,
		`{"toolCallId":"t","status":"cancelled"}`, `{"toolCallId":"t","kind":"shell"}`,
		`{"toolCallId":"t","kind":[]}`, `{"toolCallId":"t","content":{}}`,
		`{"toolCallId":"t","locations":"path"}`, `{"toolCallId":"t","_meta":[]}`,
		`{"toolCallId":"t","toolCallId":"other"}`,
	}
	for _, raw := range invalid {
		if err := permissionToolCall(json.RawMessage(raw)); !errors.Is(err, ErrProtocol) {
			t.Fatalf("malformed known tool field accepted: %s: %v", raw, err)
		}
	}
	for _, state := range []string{"in_progress", "completed", "failed"} {
		if err := permissionToolCall(json.RawMessage(`{"toolCallId":"t","status":"` + state + `"}`)); err != nil {
			t.Fatal("fixed status rejected", state, err)
		}
	}
}

func TestPermissionParserLocalCaps(t *testing.T) {
	build := func(session, tool string, options []PermissionOption) map[string]json.RawMessage {
		nativeOptions := make([]map[string]string, 0, len(options))
		for _, option := range options {
			nativeOptions = append(nativeOptions, map[string]string{"optionId": option.ID, "name": option.Name, "kind": option.Kind})
		}
		params, _ := json.Marshal(map[string]any{"sessionId": session, "toolCall": map[string]string{"toolCallId": tool}, "options": nativeOptions})
		return permissionParserFields(string(params))
	}
	option := PermissionOption{ID: strings.Repeat("i", 512), Name: strings.Repeat("n", 1024), Kind: "allow_once"}
	if _, err := parsePermission(build(strings.Repeat("s", 512), strings.Repeat("t", 512), []PermissionOption{option})); err != nil {
		t.Fatalf("exact string caps rejected: %v", err)
	}
	for _, fields := range []map[string]json.RawMessage{
		build(strings.Repeat("s", 513), "t", nil),
		build("s", strings.Repeat("t", 513), nil),
		build("s", "t", []PermissionOption{{ID: strings.Repeat("i", 513), Name: "n", Kind: "allow_once"}}),
		build("s", "t", []PermissionOption{{ID: "i", Name: strings.Repeat("n", 1025), Kind: "allow_once"}}),
	} {
		if _, err := parsePermission(fields); !errors.Is(err, ErrUnsupportedPermission) {
			t.Fatalf("over string cap error=%v", err)
		}
	}
	options := make([]PermissionOption, 0, permissionOptionLimit+1)
	for i := 0; i <= permissionOptionLimit; i++ {
		options = append(options, PermissionOption{ID: strings.Repeat("i", i), Kind: "reject_once"})
	}
	if _, err := parsePermission(build("s", "t", options[:permissionOptionLimit])); err != nil {
		t.Fatal("exact option cap rejected", err)
	}
	if _, err := parsePermission(build("s", "t", options)); !errors.Is(err, ErrUnsupportedPermission) {
		t.Fatal("over option cap accepted", err)
	}
	fields := permissionParserFields(permissionParserParams)
	fields["params"] = append(fields["params"], []byte(strings.Repeat(" ", permissionParamsByteLimit-len(fields["params"])))...)
	if _, err := parsePermission(fields); err != nil {
		t.Fatal("exact params byte cap rejected", err)
	}
	fields["params"] = append(fields["params"], ' ')
	if _, err := parsePermission(fields); !errors.Is(err, ErrUnsupportedPermission) {
		t.Fatal("over params byte cap accepted", err)
	}
}

func TestPermissionParserAndRequestDefensiveCopies(t *testing.T) {
	fields := permissionParserFields(`{"sessionId":"s","toolCall":{"toolCallId":"t","rawInput":{"private":"value"}},` +
		`"options":[{"optionId":"o","name":"private name","kind":"allow_once"}]}`)
	fields["id"] = json.RawMessage(` "\u0037" `)
	parsed, err := parsePermission(fields)
	if err != nil || parsed.key != "s:7" || string(parsed.id) != ` "\u0037" ` {
		t.Fatal("original raw id/type was not retained", err)
	}
	fields["id"][0] = 'X'
	for i := range fields["params"] {
		fields["params"][i] = 'X'
	}
	if string(parsed.id) != ` "\u0037" ` || !strings.Contains(string(parsed.toolCall), `"private":"value"`) {
		t.Fatal("parser retained caller-owned raw slices")
	}
	request := PermissionRequest{id: parsed.id, toolCall: parsed.toolCall, options: parsed.options}
	id, tool, options := request.ID(), request.ToolCall(), request.Options()
	id[0], tool[0], options[0].ID = 'X', 'X', "changed"
	options[0].rawID[0] = 'X'
	if string(request.ID()) != ` "\u0037" ` || request.ToolCall()[0] != '{' || request.Options()[0].ID != "o" || string(request.Options()[0].rawID) != `"o"` {
		t.Fatal("request accessor exposed mutable private storage")
	}
	retained := len(parsed.id) + len(parsed.key) + len(parsed.sessionID) + len(parsed.toolCall)
	for _, option := range parsed.options {
		retained += len(option.ID) + len(option.Name) + len(option.Kind) + len(option.rawID)
	}
	if parsed.bytes < retained+64*len(parsed.options) {
		t.Fatal("aggregate budget omitted retained private bytes/option overhead")
	}
}

func TestPermissionErrorsDoNotLeakPrivateCause(t *testing.T) {
	private := errors.New("private-command-and-token-sentinel")
	failure := &PermissionReplyError{Cause: private}
	if strings.Contains(failure.Error(), private.Error()) || !errors.Is(failure, private) {
		t.Fatal("error leaked private content or lost structured cause")
	}
	for _, err := range []error{ErrStalePermission, ErrInvalidPermissionAnswer, ErrUnsupportedPermission, ErrPermissionUnresolved} {
		if strings.Contains(err.Error(), private.Error()) {
			t.Fatal("public sentinel contains private content")
		}
	}
}
