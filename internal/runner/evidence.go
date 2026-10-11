package runner

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"coordica/internal/agent"
	"coordica/internal/localfile"
)

const maxEvidenceBytes = 4 << 20
const evidenceTailReserve = 128 << 10

// evidence is private local diagnostic evidence. It is never interpreted as
// authorization or uploaded wholesale to the center.
type evidence struct {
	file      *os.File
	written   int
	truncated bool
	err       error
	secret    string
}

func newEvidence(path, secret string) (*evidence, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	if err = localfile.Protect(path); err != nil {
		f.Close()
		return nil, err
	}
	return &evidence{file: f, secret: secret}, nil
}
func (e *evidence) append(event agent.Event, terminal bool) {
	if e.err != nil {
		return
	}
	event = agent.SanitizeEvent(event, e.secret)
	raw, err := json.Marshal(struct {
		Time time.Time `json:"time"`
		agent.Event
	}{time.Now().UTC(), event})
	if err != nil {
		e.err = err
		return
	}
	limit := maxEvidenceBytes - evidenceTailReserve
	if terminal {
		limit = maxEvidenceBytes
	}
	if e.written+len(raw)+1 > limit {
		if !e.truncated {
			e.truncated = true
			marker := agent.Event{Type: "runner.log_truncated", Data: json.RawMessage(`{"limit_bytes":4194304,"detail":"complete events exceeding the limit were omitted; final output has reserved space"}`)}
			e.append(marker, true)
		}
		return
	}
	raw = append(raw, '\n')
	_, e.err = e.file.Write(raw)
	e.written += len(raw)
}
func (e *evidence) value(kind string, value any, terminal bool) {
	raw, err := json.Marshal(value)
	if err != nil {
		e.err = err
		return
	}
	e.append(agent.Event{Type: kind, Data: raw}, terminal)
}
func (e *evidence) close() error {
	err := e.file.Sync()
	return errors.Join(e.err, err, e.file.Close())
}
func boundedText(text string, limit int) (string, bool) {
	text = strings.ToValidUTF8(text, "�")
	if len(text) <= limit {
		return text, false
	}
	end := limit
	for end > 0 && !utf8.RuneStart(text[end]) {
		end--
	}
	return text[:end], true
}
func (r *Runner) safeText(text string) string {
	raw, _ := json.Marshal(text)
	event := agent.SanitizeEvent(agent.Event{Type: "text", Data: raw}, r.config.Token)
	var safe string
	json.Unmarshal(event.Data, &safe)
	return safe
}
