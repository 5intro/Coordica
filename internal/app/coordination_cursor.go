package app

import (
	"encoding/base64"
	"encoding/json"

	"coordica/internal/domain"
	"coordica/internal/store"
)

type workCursor struct {
	Version   int    `json:"v"`
	Epoch     string `json:"e"`
	Binding   string `json:"b"`
	Upper     int64  `json:"u"`
	Before    int64  `json:"n"`
	Reference string `json:"r,omitempty"`
}

func (r workReader) cursorBinding(kind string, anchor domain.WorkAnchor, filter string) string {
	data, _ := json.Marshal([]string{r.project.ID, r.actor.Role, r.actor.ID, r.binding.ChatScope, r.binding.NodeID, kind, anchor.Kind, anchor.ID, filter})
	return Hash(string(data))
}

func (r workReader) cursor(raw, kind string, anchor domain.WorkAnchor, filter string) (workCursor, error) {
	c := workCursor{Version: 1, Epoch: r.epoch, Binding: r.cursorBinding(kind, anchor, filter)}
	if raw == "" {
		var err error
		c.Upper, err = store.WorkUpper(r.tx, r.project.ID, r.authorFilter())
		return c, err
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(data) > 2048 {
		return c, domain.Fail("invalid_cursor", "refresh work history from its first page")
	}
	var saved workCursor
	if err = json.Unmarshal(data, &saved); err != nil || saved.Version != 1 || saved.Epoch != c.Epoch || saved.Binding != c.Binding ||
		saved.Upper < 0 || saved.Before < 0 || saved.Before > saved.Upper || len(saved.Reference) > 300 {
		return c, domain.Fail("invalid_cursor", "work history cursor belongs to another scope, query or restored instance")
	}
	upper, err := store.WorkUpper(r.tx, r.project.ID, r.authorFilter())
	if err != nil {
		return c, err
	}
	if saved.Upper > upper {
		return c, domain.Fail("invalid_cursor", "work history has changed; refresh the first page")
	}
	return saved, nil
}

func encodeWorkCursor(c workCursor) string {
	data, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(data)
}
