package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"runtime"
	"strings"
)

type acpLocation struct {
	Path string `json:"path"`
}
type acpToolCall struct {
	ID        string        `json:"toolCallId"`
	Kind      string        `json:"kind"`
	Locations []acpLocation `json:"locations"`
}
type acpMetadata struct {
	call    acpToolCall
	invalid bool
}

// Some peers send full typed metadata in a tool_call notification and only an
// id/display patch in request_permission. Missing evidence can be filled from
// the same session's call, while conflicts never broaden a grant.
func mergeACPCall(existing acpMetadata, patch acpToolCall) acpMetadata {
	if existing.invalid || (existing.call.ID != "" && patch.ID != existing.call.ID) {
		return acpMetadata{invalid: true}
	}
	existing.call.ID = patch.ID
	if patch.Kind != "" {
		if existing.call.Kind != "" && existing.call.Kind != patch.Kind {
			return acpMetadata{invalid: true}
		}
		existing.call.Kind = patch.Kind
	}
	if patch.Locations != nil {
		if existing.call.Locations != nil {
			old, _ := json.Marshal(existing.call.Locations)
			next, _ := json.Marshal(patch.Locations)
			if !bytes.Equal(old, next) {
				return acpMetadata{invalid: true}
			}
		}
		existing.call.Locations = append([]acpLocation(nil), patch.Locations...)
	}
	return existing
}

func recordACPCall(calls map[string]acpMetadata, raw json.RawMessage) error {
	var call acpToolCall
	if json.Unmarshal(raw, &call) != nil || call.ID == "" {
		return ErrProtocol
	}
	if _, found := calls[call.ID]; !found && len(calls) >= 1024 {
		return errors.New("ACP tool metadata limit exceeded")
	}
	if len(call.Locations) > 256 {
		return errors.New("ACP tool location limit exceeded")
	}
	calls[call.ID] = mergeACPCall(calls[call.ID], call)
	return nil
}

// Permission is based on structured operation kind and canonical locations,
// never a title or model description. This bounds local grants; it does not
// provide an OS sandbox or replace the center's actual-diff scope check.
func acpPermission(req Request, sessionID string, raw json.RawMessage) (string, bool) {
	return acpPermissionFromCalls(req, sessionID, raw, nil)
}

func acpPermissionFromCalls(req Request, sessionID string, raw json.RawMessage, calls map[string]acpMetadata) (string, bool) {
	var params struct {
		SessionID string      `json:"sessionId"`
		ToolCall  acpToolCall `json:"toolCall"`
		Options   []struct {
			ID   string `json:"optionId"`
			Kind string `json:"kind"`
		} `json:"options"`
	}
	if json.Unmarshal(raw, &params) != nil || params.SessionID != sessionID || params.ToolCall.ID == "" {
		return "", false
	}
	merged := mergeACPCall(calls[params.ToolCall.ID], params.ToolCall)
	call := merged.call
	if merged.invalid || len(call.Locations) == 0 {
		return "", false
	}
	tools := req.AllowedTools
	if isReview(req) || len(tools) == 0 {
		tools = []string{"Read", "Glob", "Grep"}
	}
	allowed := false
	for _, tool := range tools {
		switch call.Kind {
		case "read":
			allowed = allowed || tool == "Read"
		case "search":
			allowed = allowed || tool == "Glob" || tool == "Grep"
		case "edit":
			allowed = allowed || (!isReview(req) && (tool == "Edit" || tool == "Write"))
		}
	}
	if !allowed {
		return "", false
	}
	for _, loc := range call.Locations {
		if !withinWorkDir(req.WorkDir, loc.Path) {
			return "", false
		}
	}
	for _, option := range params.Options {
		if option.Kind == "allow_once" && option.ID != "" {
			return option.ID, true
		}
	}
	return "", false
}

func withinWorkDir(root, path string) bool {
	if !filepath.IsAbs(path) {
		return false
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false
	}
	current := filepath.Clean(path)
	var suffix []string
	for {
		canonical, err := filepath.EvalSymlinks(current)
		if err == nil {
			for i := len(suffix) - 1; i >= 0; i-- {
				canonical = filepath.Join(canonical, suffix[i])
			}
			if runtime.GOOS == "windows" {
				canonicalRoot = strings.ToLower(canonicalRoot)
				canonical = strings.ToLower(canonical)
			}
			rel, err := filepath.Rel(canonicalRoot, canonical)
			return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return false
		}
		suffix = append(suffix, filepath.Base(current))
		current = parent
	}
}
