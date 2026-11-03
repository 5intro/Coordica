package acpclient

import (
	"bytes"
	"encoding/json"
	"unicode/utf8"

	"coordica/internal/jsoninput"
)

func object(raw []byte) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if !utf8.Valid(raw) || jsoninput.DecodeNullable(raw, &fields, maxFrameBytes) != nil {
		return nil, ErrProtocol
	}
	if meta, exists := fields["_meta"]; exists && !bytes.Equal(bytes.TrimSpace(meta), []byte("null")) {
		var value map[string]json.RawMessage
		if json.Unmarshal(meta, &value) != nil || value == nil {
			return nil, ErrProtocol
		}
	}
	return fields, nil
}

func decode(fields map[string]json.RawMessage, name string, value any, required bool) error {
	raw, exists := fields[name]
	if !exists && !required {
		return nil
	}
	if !exists || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, value) != nil {
		return ErrProtocol
	}
	return nil
}

func (p *Peer) receive(raw []byte) error {
	fields, err := object(raw)
	if err != nil {
		return err
	}
	for name := range fields {
		if name != "jsonrpc" && name != "id" && name != "result" && name != "error" {
			return ErrProtocol
		}
	}
	var version string
	if decode(fields, "jsonrpc", &version, true) != nil || version != "2.0" {
		return ErrProtocol
	}
	result, hasResult := fields["result"]
	rpc, hasError := fields["error"]
	if hasResult == hasError {
		return ErrProtocol
	}
	if string(fields["id"]) == "2" {
		return p.receiveSession(result, rpc, hasError)
	}
	if string(fields["id"]) != "1" {
		return ErrProtocol
	}
	p.mu.Lock()
	if !p.pending {
		p.mu.Unlock()
		return ErrProtocol
	}
	p.pending = false
	p.mu.Unlock()
	var reply initializeReply
	if hasError {
		value, err := object(rpc)
		if err != nil || jsoninput.Required(rpc, "code", "message") != nil {
			return ErrProtocol
		}
		var rejection RPCError
		if decode(value, "code", &rejection.Code, true) != nil || decode(value, "message", &rejection.Message, true) != nil {
			return ErrProtocol
		}
		rejection.Data = append(json.RawMessage(nil), value["data"]...)
		reply.err = &rejection
	} else {
		info, err := initializationInfo(result)
		if err != nil && err != ErrUnsupportedVersion {
			return err
		}
		reply.info, reply.err = info, err
		if err == nil {
			p.mu.Lock()
			p.info = info
			p.mu.Unlock()
		}
	}
	p.reply <- reply
	return nil
}

func initializationInfo(raw []byte) (Info, error) {
	fields, err := object(raw)
	if err != nil {
		return Info{}, err
	}
	var info Info
	if decode(fields, "protocolVersion", &info.ProtocolVersion, true) != nil || info.ProtocolVersion < 0 || info.ProtocolVersion > 65535 {
		return Info{}, ErrProtocol
	}
	if info.ProtocolVersion != 1 {
		return Info{}, ErrUnsupportedVersion
	}
	if raw, exists := fields["agentCapabilities"]; exists {
		caps, err := object(raw)
		if err != nil || decode(caps, "loadSession", &info.AgentCapabilities.LoadSession, false) != nil {
			return Info{}, ErrProtocol
		}
		groups := map[string]map[string]*bool{
			"promptCapabilities":  {"audio": &info.AgentCapabilities.Prompt.Audio, "image": &info.AgentCapabilities.Prompt.Image, "embeddedContext": &info.AgentCapabilities.Prompt.EmbeddedContext},
			"mcpCapabilities":     {"http": &info.AgentCapabilities.MCP.HTTP, "sse": &info.AgentCapabilities.MCP.SSE},
			"sessionCapabilities": {},
		}
		for group, options := range groups {
			if raw, exists := caps[group]; exists {
				values, err := object(raw)
				if err != nil {
					return Info{}, err
				}
				for name, value := range options {
					if decode(values, name, value, false) != nil {
						return Info{}, ErrProtocol
					}
				}
			}
		}
	}
	if raw, exists := fields["agentInfo"]; exists && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		values, err := object(raw)
		if err != nil {
			return Info{}, err
		}
		info.AgentInfo = &Implementation{}
		if decode(values, "name", &info.AgentInfo.Name, true) != nil || decode(values, "version", &info.AgentInfo.Version, true) != nil || len(info.AgentInfo.Name) > 512 || len(info.AgentInfo.Version) > 512 {
			return Info{}, ErrProtocol
		}
		if title, exists := values["title"]; exists && !bytes.Equal(bytes.TrimSpace(title), []byte("null")) {
			var value string
			if json.Unmarshal(title, &value) != nil {
				return Info{}, ErrProtocol
			}
		}
	}
	if _, exists := fields["authMethods"]; exists {
		var methods []json.RawMessage
		if decode(fields, "authMethods", &methods, true) != nil || len(methods) > 64 {
			return Info{}, ErrProtocol
		}
		seen := map[string]bool{}
		for _, raw := range methods {
			values, err := object(raw)
			if err != nil {
				return Info{}, err
			}
			var method AuthMethod
			if decode(values, "id", &method.ID, true) != nil || decode(values, "name", &method.Name, true) != nil || len(method.ID) > 512 || len(method.Name) > 512 || seen[method.ID] {
				return Info{}, ErrProtocol
			}
			if description, exists := values["description"]; exists && !bytes.Equal(bytes.TrimSpace(description), []byte("null")) {
				var value string
				if json.Unmarshal(description, &value) != nil {
					return Info{}, ErrProtocol
				}
			}
			seen[method.ID] = true
			info.AuthMethods = append(info.AuthMethods, method)
		}
	}
	return info, nil
}
