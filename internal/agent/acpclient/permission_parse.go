package acpclient

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"unicode/utf8"
)

const permissionParamsByteLimit = 64 << 10
const permissionOptionLimit = 32

type parsedPermission struct {
	id        json.RawMessage
	key       string
	sessionID string
	toolCall  json.RawMessage
	options   []PermissionOption
	bytes     int
}

func parsePermission(fields map[string]json.RawMessage) (parsedPermission, error) {
	var parsed parsedPermission
	for name := range fields {
		if name != "jsonrpc" && name != "method" && name != "id" && name != "params" {
			return parsed, ErrProtocol
		}
	}
	var version, method string
	if decode(fields, "jsonrpc", &version, true) != nil || version != "2.0" ||
		decode(fields, "method", &method, true) != nil || method != "session/request_permission" {
		return parsed, ErrProtocol
	}
	id, exists := fields["id"]
	if !exists {
		return parsed, ErrProtocol
	}
	key, err := permissionID(id)
	if err != nil {
		return parsed, err
	}
	if len(fields["params"]) > permissionParamsByteLimit {
		return parsed, ErrUnsupportedPermission
	}
	params, err := object(fields["params"])
	if err != nil || decode(params, "sessionId", &parsed.sessionID, true) != nil {
		return parsed, ErrProtocol
	}
	if len(parsed.sessionID) > 512 {
		return parsedPermission{}, ErrUnsupportedPermission
	}
	if err := permissionToolCall(params["toolCall"]); err != nil {
		return parsedPermission{}, err
	}
	options, err := permissionOptions(params)
	if err != nil {
		return parsedPermission{}, err
	}
	parsed.id, parsed.key = append(json.RawMessage(nil), id...), key
	parsed.toolCall = append(json.RawMessage(nil), params["toolCall"]...)
	parsed.options = options
	// Include retained strings/raw values and conservative per-entry overhead.
	// The caller applies a separate aggregate budget before publishing handles.
	parsed.bytes = 128 + len(parsed.id) + len(parsed.key) + len(parsed.sessionID) + len(parsed.toolCall)
	for _, option := range options {
		parsed.bytes += 64 + len(option.ID) + len(option.Name) + len(option.Kind) + len(option.rawID)
	}
	return parsed, nil
}

// permissionID keeps string/number/null namespaces distinct without float64.
// Decimal/exponent numeric spellings are a deliberately unsupported local
// subset; the fixed schema does not prohibit every such integer spelling.
func permissionID(raw json.RawMessage) (string, error) {
	value := bytes.TrimSpace(raw)
	if !utf8.Valid(value) || !json.Valid(value) {
		return "", ErrProtocol
	}
	if bytes.Equal(value, []byte("null")) {
		return "null", nil
	}
	if value[0] == '"' {
		var id string
		if json.Unmarshal(value, &id) != nil {
			return "", ErrProtocol
		}
		if len(id) > 512 {
			return "", ErrUnsupportedPermission
		}
		return "s:" + id, nil
	}
	if value[0] != '-' && (value[0] < '0' || value[0] > '9') {
		return "", ErrProtocol
	}
	if strings.ContainsAny(string(value), ".eE") {
		return "", ErrUnsupportedPermission
	}
	id, err := strconv.ParseInt(string(value), 10, 64)
	if err != nil {
		return "", ErrProtocol
	}
	return "n:" + strconv.FormatInt(id, 10), nil
}

func permissionOptions(params map[string]json.RawMessage) ([]PermissionOption, error) {
	var rawOptions []json.RawMessage
	if decode(params, "options", &rawOptions, true) != nil {
		return nil, ErrProtocol
	}
	if len(rawOptions) > permissionOptionLimit {
		return nil, ErrUnsupportedPermission
	}
	options := make([]PermissionOption, 0, len(rawOptions))
	seen := make(map[string]bool, len(rawOptions))
	for _, raw := range rawOptions {
		fields, err := object(raw)
		var option PermissionOption
		if err != nil || decode(fields, "optionId", &option.ID, true) != nil ||
			decode(fields, "name", &option.Name, true) != nil || decode(fields, "kind", &option.Kind, true) != nil {
			return nil, ErrProtocol
		}
		if len(option.ID) > 512 || len(option.Name) > 1024 || seen[option.ID] {
			return nil, ErrUnsupportedPermission
		}
		switch option.Kind {
		case "allow_once", "allow_always", "reject_once", "reject_always":
		default:
			return nil, ErrProtocol
		}
		seen[option.ID] = true
		option.rawID = append(json.RawMessage(nil), fields["optionId"]...)
		options = append(options, option)
	}
	return options, nil
}

// permissionToolCall validates only the declared shallow subset. Content and
// location items, raw tool parameters and extensions remain opaque private
// native data; accepting their containers does not validate full ToolCallContent
// schemas, commands, paths or authorization scope.
func permissionToolCall(raw json.RawMessage) error {
	fields, err := object(raw)
	var id string
	if err != nil || decode(fields, "toolCallId", &id, true) != nil {
		return ErrProtocol
	}
	if len(id) > 512 {
		return ErrUnsupportedPermission
	}
	if err := permissionNullableString(fields, "title"); err != nil {
		return err
	}
	if err := permissionNullableString(fields, "status", "pending", "in_progress", "completed", "failed"); err != nil {
		return err
	}
	if err := permissionNullableString(fields, "kind",
		"read", "edit", "delete", "move", "search", "execute", "think", "fetch", "switch_mode", "other"); err != nil {
		return err
	}
	for _, name := range []string{"content", "locations"} {
		value, exists := fields[name]
		if !exists || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			continue
		}
		var items []json.RawMessage
		if json.Unmarshal(value, &items) != nil {
			return ErrProtocol
		}
	}
	return nil
}

func permissionNullableString(fields map[string]json.RawMessage, name string, allowed ...string) error {
	raw, exists := fields[name]
	if !exists || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return ErrProtocol
	}
	if len(allowed) == 0 {
		return nil
	}
	for _, candidate := range allowed {
		if value == candidate {
			return nil
		}
	}
	return ErrProtocol
}
