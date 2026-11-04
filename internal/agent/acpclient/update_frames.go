package acpclient

import (
	"bytes"
	"encoding/json"
	"strings"
	"unicode"
)

func (p *Peer) receiveUpdate(fields map[string]json.RawMessage) error {
	for name := range fields {
		if name != "jsonrpc" && name != "method" && name != "params" {
			p.updates.fault("unsupported_callback")
			return ErrUnsupportedUpdate
		}
	}
	var method string
	if decode(fields, "method", &method, true) != nil {
		return ErrProtocol
	}
	if method != "session/update" {
		p.updates.fault("unsupported_notification")
		return ErrUnsupportedUpdate
	}
	params, err := object(fields["params"])
	if err != nil {
		return err
	}
	var sessionID string
	if decode(params, "sessionId", &sessionID, true) != nil {
		return ErrProtocol
	}
	p.mu.Lock()
	ready, id := p.sessionReady, p.sessionID
	p.mu.Unlock()
	if !ready {
		p.updates.fault("update_before_session")
		return ErrUnsupportedUpdate
	}
	if sessionID != id {
		p.updates.fault("session_mismatch")
		return ErrProtocol
	}
	update, err := object(params["update"])
	if err != nil {
		return err
	}
	var kind string
	if decode(update, "sessionUpdate", &kind, true) != nil {
		return ErrProtocol
	}
	if kind == "config_option_update" {
		options, err := requiredConfigOptions(update)
		if err != nil {
			return err
		}
		p.mu.Lock()
		version := p.replaceConfigLocked(options, true).Version
		p.mu.Unlock()
		return p.updates.append(UpdateFact{Kind: kind, ConfigVersion: version})
	}
	fact, err := updateFact(update)
	if err != nil {
		if err == ErrUnsupportedUpdate {
			p.updates.fault("unsupported_update")
		}
		return err
	}
	return p.updates.append(fact)
}

func updateFact(update map[string]json.RawMessage) (UpdateFact, error) {
	var fact UpdateFact
	if decode(update, "sessionUpdate", &fact.Kind, true) != nil {
		return fact, ErrProtocol
	}
	switch fact.Kind {
	case "user_message_chunk", "agent_message_chunk", "agent_thought_chunk":
		content, err := object(update["content"])
		if err != nil {
			return UpdateFact{}, err
		}
		var kind, text string
		if decode(content, "type", &kind, true) != nil {
			return UpdateFact{}, ErrProtocol
		}
		if kind != "text" {
			return UpdateFact{}, ErrUnsupportedUpdate
		}
		if decode(content, "text", &text, true) != nil {
			return UpdateFact{}, ErrProtocol
		}
		fact.TextBytes = int64(len(text))
	case "tool_call", "tool_call_update":
		if decode(update, "toolCallId", &fact.ToolCallID, true) != nil || fact.ToolCallID == "" || len(fact.ToolCallID) > 512 || strings.ContainsFunc(fact.ToolCallID, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
			return UpdateFact{}, ErrProtocol
		}
		var title string
		_, statusProvided := update["status"]
		if fact.Kind == "tool_call" {
			if decode(update, "title", &title, true) != nil || decode(update, "status", &fact.State, false) != nil {
				return UpdateFact{}, ErrProtocol
			}
		} else {
			if value, exists := update["status"]; !exists || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				statusProvided = false
			}
			if value, exists := update["title"]; exists && !bytes.Equal(bytes.TrimSpace(value), []byte("null")) && json.Unmarshal(value, &title) != nil {
				return UpdateFact{}, ErrProtocol
			}
			if value, exists := update["status"]; exists && !bytes.Equal(bytes.TrimSpace(value), []byte("null")) && json.Unmarshal(value, &fact.State) != nil {
				return UpdateFact{}, ErrProtocol
			}
		}
		if statusProvided && fact.State != "pending" && fact.State != "in_progress" && fact.State != "completed" && fact.State != "failed" {
			return UpdateFact{}, ErrProtocol
		}
	case "plan":
		if err := planUpdate(update); err != nil {
			return UpdateFact{}, err
		}
	case "available_commands_update":
		if err := commandsUpdate(update); err != nil {
			return UpdateFact{}, err
		}
	case "current_mode_update":
		var id string // Open native ID, not a local allowlist or permission grant.
		if decode(update, "currentModeId", &id, true) != nil {
			return UpdateFact{}, ErrProtocol
		}
	default:
		return UpdateFact{}, ErrUnsupportedUpdate
	}
	return fact, nil
}
