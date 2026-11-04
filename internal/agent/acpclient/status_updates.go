package acpclient

import (
	"bytes"
	"encoding/json"
)

// Status declarations are validated inside the existing bounded frame, then
// discarded. Only their kind enters the metadata FIFO: no plan dispatch,
// command invocation, mode selection or permission follows from these reports.
func planUpdate(fields map[string]json.RawMessage) error {
	var entries []json.RawMessage
	if decode(fields, "entries", &entries, true) != nil {
		return ErrProtocol
	}
	for _, raw := range entries {
		entry, err := object(raw)
		if err != nil {
			return err
		}
		var content, priority, status string
		if decode(entry, "content", &content, true) != nil ||
			decode(entry, "priority", &priority, true) != nil || decode(entry, "status", &status, true) != nil {
			return ErrProtocol
		}
		if priority != "high" && priority != "medium" && priority != "low" {
			return ErrProtocol
		}
		if status != "pending" && status != "in_progress" && status != "completed" {
			return ErrProtocol
		}
	}
	return nil
}

func commandsUpdate(fields map[string]json.RawMessage) error {
	var commands []json.RawMessage
	if decode(fields, "availableCommands", &commands, true) != nil {
		return ErrProtocol
	}
	for _, raw := range commands {
		command, err := object(raw)
		if err != nil {
			return err
		}
		var name, description string
		if decode(command, "name", &name, true) != nil || decode(command, "description", &description, true) != nil {
			return ErrProtocol
		}
		if raw, exists := command["input"]; exists && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			input, err := object(raw)
			if err != nil {
				return err
			}
			var hint string
			if decode(input, "hint", &hint, true) != nil {
				return ErrProtocol
			}
		}
	}
	return nil
}
