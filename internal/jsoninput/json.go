// Package jsoninput validates bounded, unambiguous transport objects.
package jsoninput

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

func Decode(raw []byte, value any, max int) error {
	return decode(raw, value, max, false)
}

// DecodeNullable retains duplicate, unknown-field and size checks for external
// read-only reports whose protocol uses null to mean an unestablished result.
// Consumers must validate required result fields before trusting the report.
// Command transports continue to use Decode, which rejects null everywhere.
func DecodeNullable(raw []byte, value any, max int) error {
	return decode(raw, value, max, true)
}

func decode(raw []byte, value any, max int, allowNull bool) error {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || len(raw) > max || raw[0] != '{' {
		return errors.New("invalid JSON object")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	if err := unique(d, allowNull); err != nil {
		return errors.New("invalid or duplicate JSON fields")
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("trailing JSON")
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		return errors.New("invalid JSON fields")
	}
	return nil
}
func unique(d *json.Decoder, allowNull bool) error {
	t, e := d.Token()
	if e != nil {
		return e
	}
	if t == nil {
		if allowNull {
			return nil
		}
		return errors.New("null field")
	}
	delim, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	seen := map[string]bool{}
	for d.More() {
		if delim == '{' {
			k, e := d.Token()
			if e != nil {
				return e
			}
			key, ok := k.(string)
			if !ok || seen[key] {
				return errors.New("duplicate field")
			}
			seen[key] = true
		}
		if e := unique(d, allowNull); e != nil {
			return e
		}
	}
	_, e = d.Token()
	return e
}

// Required rejects omitted/null gates even when Go's zero value is meaningful.
func Required(raw []byte, names ...string) error {
	var fields map[string]json.RawMessage
	if e := json.Unmarshal(raw, &fields); e != nil {
		return errors.New("invalid JSON object")
	}
	for _, name := range names {
		v, ok := fields[name]
		if !ok || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return errors.New("required JSON field missing")
		}
	}
	return nil
}
