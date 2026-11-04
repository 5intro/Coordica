package acpclient

import (
	"bytes"
	"encoding/json"
	"errors"
)

var ErrConfigUnavailable = errors.New("ACP configuration is outside this client's advertised selection subset")
var ErrConfigMismatch = errors.New("ACP configuration response did not confirm the requested selection")

type ConfigChoice struct {
	Value, Name string
	Description *string
}
type ConfigGroup struct {
	Group, Name string
	Options     []ConfigChoice
}
type ConfigOption struct {
	ID, Name, CurrentValue string
	Category, Description  *string // Native UX metadata, never authority or model mapping.
	Choices                []ConfigChoice
	Groups                 []ConfigGroup
}

// ConfigSnapshot is a bounded, historical native declaration. Available=false
// means absent/null, while an available empty list is a known empty catalogue.
// Version is local receive order, not server CAS or proof of effective settings.
type ConfigSnapshot struct {
	Version             uint64
	Evidence, SessionID string
	Available           bool
	Options             []ConfigOption
}

func copyText(v *string) *string {
	if v == nil {
		return nil
	}
	x := *v
	return &x
}
func copyChoices(v []ConfigChoice) []ConfigChoice {
	result := append([]ConfigChoice(nil), v...)
	for i := range result {
		result[i].Description = copyText(result[i].Description)
	}
	return result
}
func copyConfig(v ConfigSnapshot) ConfigSnapshot {
	v.Options = append([]ConfigOption(nil), v.Options...)
	for i := range v.Options {
		o := &v.Options[i]
		o.Category, o.Description = copyText(o.Category), copyText(o.Description)
		o.Choices = copyChoices(o.Choices)
		o.Groups = append([]ConfigGroup(nil), o.Groups...)
		for j := range o.Groups {
			o.Groups[j].Options = copyChoices(o.Groups[j].Options)
		}
	}
	return v
}

// ConfigOptions reads only the original Session's private historical snapshot.
// It does not query the native agent and remains readable after disconnect.
func (s Session) ConfigOptions() (ConfigSnapshot, error) {
	if _, err := s.capture(); err != nil {
		return ConfigSnapshot{}, err
	}
	p := s.peer
	p.mu.Lock()
	defer p.mu.Unlock()
	return copyConfig(p.configuration), nil
}

func (p *Peer) replaceConfigLocked(options []ConfigOption, available bool) ConfigSnapshot {
	p.configuration = ConfigSnapshot{Version: p.configuration.Version + 1, Evidence: "native_reported", SessionID: p.sessionID, Available: available, Options: options}
	return copyConfig(p.configuration)
}

func optionalConfigOptions(fields map[string]json.RawMessage) ([]ConfigOption, bool, error) {
	raw, exists := fields["configOptions"]
	if !exists || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, false, nil
	}
	v, err := parseConfigOptions(raw)
	return v, true, err
}
func requiredConfigOptions(fields map[string]json.RawMessage) ([]ConfigOption, error) {
	raw, exists := fields["configOptions"]
	if !exists || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, ErrProtocol
	}
	return parseConfigOptions(raw)
}

func configText(fields map[string]json.RawMessage, name string, max int) (string, error) {
	var value string
	if decode(fields, name, &value, true) != nil {
		return "", ErrProtocol
	}
	if len(value) > max {
		return "", ErrConfigUnavailable
	}
	return value, nil
}
func configOptionalText(fields map[string]json.RawMessage, name string) (*string, error) {
	if raw, ok := fields[name]; !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	v, err := configText(fields, name, 2048)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func parseConfigOptions(raw []byte) ([]ConfigOption, error) {
	var entries []json.RawMessage
	if json.Unmarshal(raw, &entries) != nil || entries == nil {
		return nil, ErrProtocol
	}
	if len(raw) > 64<<10 || len(entries) > 64 {
		return nil, ErrConfigUnavailable
	}
	options := make([]ConfigOption, 0, len(entries))
	seen := map[string]bool{}
	count := 0
	for _, entry := range entries {
		fields, err := object(entry)
		if err != nil {
			return nil, err
		}
		var o ConfigOption
		if o.ID, err = configText(fields, "id", 512); err != nil {
			return nil, err
		}
		if o.ID == "" || seen[o.ID] {
			return nil, ErrConfigUnavailable
		}
		seen[o.ID] = true
		if o.Name, err = configText(fields, "name", 1024); err != nil {
			return nil, err
		}
		if o.CurrentValue, err = configText(fields, "currentValue", 2048); err != nil {
			return nil, err
		}
		kind, err := configText(fields, "type", 64)
		if err != nil {
			return nil, err
		}
		if kind != "select" {
			return nil, ErrConfigUnavailable
		}
		if o.Category, err = configOptionalText(fields, "category"); err != nil {
			return nil, err
		}
		if o.Description, err = configOptionalText(fields, "description"); err != nil {
			return nil, err
		}
		if o.Choices, o.Groups, err = configChoices(fields["options"], &count); err != nil {
			return nil, err
		}
		options = append(options, o)
	}
	return options, nil
}

func configChoices(raw []byte, total *int) ([]ConfigChoice, []ConfigGroup, error) {
	var entries []json.RawMessage
	if json.Unmarshal(raw, &entries) != nil || entries == nil {
		return nil, nil, ErrProtocol
	}
	choices := []ConfigChoice{}
	groups := []ConfigGroup{}
	seen := map[string]bool{}
	grouped := false
	for i, entry := range entries {
		fields, err := object(entry)
		if err != nil {
			return nil, nil, err
		}
		_, isGroup := fields["group"]
		_, isValue := fields["value"]
		if isGroup == isValue || (i > 0 && grouped != isGroup) {
			return nil, nil, ErrProtocol
		}
		grouped = isGroup
		if !grouped {
			c, err := configChoice(fields, seen, total)
			if err != nil {
				return nil, nil, err
			}
			choices = append(choices, c)
			continue
		}
		var g ConfigGroup
		if g.Group, err = configText(fields, "group", 512); err != nil {
			return nil, nil, err
		}
		if g.Name, err = configText(fields, "name", 1024); err != nil {
			return nil, nil, err
		}
		var nested []json.RawMessage
		if decode(fields, "options", &nested, true) != nil {
			return nil, nil, ErrProtocol
		}
		for _, child := range nested {
			f, err := object(child)
			if err != nil {
				return nil, nil, err
			}
			c, err := configChoice(f, seen, total)
			if err != nil {
				return nil, nil, err
			}
			g.Options = append(g.Options, c)
		}
		groups = append(groups, g)
	}
	return choices, groups, nil
}
func configChoice(fields map[string]json.RawMessage, seen map[string]bool, total *int) (ConfigChoice, error) {
	var c ConfigChoice
	var err error
	if _, nested := fields["group"]; nested {
		return c, ErrProtocol
	}
	if c.Value, err = configText(fields, "value", 2048); err != nil {
		return c, err
	}
	if seen[c.Value] {
		return c, ErrConfigUnavailable
	}
	seen[c.Value] = true
	if c.Name, err = configText(fields, "name", 1024); err != nil {
		return c, err
	}
	if c.Description, err = configOptionalText(fields, "description"); err != nil {
		return c, err
	}
	*total++
	if *total > 256 {
		return c, ErrConfigUnavailable
	}
	return c, nil
}
