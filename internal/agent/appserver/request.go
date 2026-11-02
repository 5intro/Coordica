package appserver

import (
	"encoding/json"
	"errors"
	"unicode/utf8"

	"coordica/internal/jsoninput"
)

var (
	ErrUnsupported   = errors.New("native app-server operation is outside this typed adapter's supported subset")
	ErrInvalidAnswer = errors.New("answer does not match the native request")
)

type QuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

// Question preserves native IDs and secret/other flags. Its content and answers
// may be private; the application must redact before persistence or display.
type Question struct {
	ID       string           `json:"id"`
	Header   string           `json:"header"`
	Text     string           `json:"question"`
	IsOther  bool             `json:"isOther"`
	IsSecret bool             `json:"isSecret"`
	Options  []QuestionOption `json:"options"`
}

// Request is an immutable connection/Thread/Turn callback. Creating it grants
// no authority to approve; the application must validate its current principal,
// persisted request version and Task/Run authorization before replying.
type Request struct {
	frame          Frame
	questions      []Question
	decisions      []json.RawMessage
	decisionSource string
	blocking       bool
}

func (r Request) ID() json.RawMessage            { return append(json.RawMessage{}, r.frame.ID...) }
func (r Request) Method() string                 { return r.frame.Method }
func (r Request) DecisionSource() string         { return r.decisionSource }
func (r Request) QuestionBlocking() (bool, bool) { return r.blocking, len(r.questions) > 0 }

// Parameters returns a defensive private-data copy for caller-owned diagnostic
// and presentation policy, including command, cwd, reason and native item ID.
func (r Request) Parameters() json.RawMessage { return append(json.RawMessage{}, r.frame.Params...) }

// Request checks the exact native target and connection. Unknown methods remain
// available as raw Frames, never converted to an approval or guessed question.
func (t Turn) Request(frame Frame) (Request, error) {
	if !t.valid() || frame.owner != t.thread.peer || frame.ID == nil {
		return Request{}, ErrStaleRequest
	}
	if !t.thread.peer.answerable(frame) {
		return Request{}, ErrStaleRequest
	}
	if _, err := idKey(frame.ID); err != nil {
		return Request{}, err
	}
	scope := nativeScope(frame.Params)
	if scope.ThreadID != t.thread.id || scope.TurnID != t.id {
		return Request{}, ErrStaleRequest
	}
	if jsoninput.Required(frame.Params, "threadId", "turnId", "itemId") != nil {
		return Request{}, ErrProtocol
	}
	var identity struct {
		ItemID    string `json:"itemId"`
		StartedAt *int64 `json:"startedAtMs"`
	}
	if json.Unmarshal(frame.Params, &identity) != nil || !validNativeID(identity.ItemID) {
		return Request{}, ErrProtocol
	}
	r := Request{frame: frame}
	r.frame.ID = append(json.RawMessage{}, frame.ID...)
	r.frame.Params = append(json.RawMessage{}, frame.Params...)
	switch frame.Method {
	case "item/tool/requestUserInput":
		if !t.thread.peer.experimental {
			return Request{}, ErrUnsupported
		}
		if err := r.parseQuestions(); err != nil {
			return Request{}, err
		}
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval":
		if jsoninput.Required(frame.Params, "startedAtMs") != nil {
			return Request{}, ErrProtocol
		}
		var value struct {
			Decisions []json.RawMessage `json:"availableDecisions"`
		}
		if json.Unmarshal(frame.Params, &value) != nil {
			return Request{}, ErrProtocol
		}
		r.decisions, r.decisionSource = value.Decisions, "native"
		if value.Decisions == nil {
			// These four values are the pinned schema's basic decisions, not
			// evidence that the server advertised a richer policy amendment.
			r.decisions = []json.RawMessage{
				json.RawMessage(`"accept"`), json.RawMessage(`"acceptForSession"`),
				json.RawMessage(`"decline"`), json.RawMessage(`"cancel"`),
			}
			r.decisionSource = "schema_default"
		}
	default:
		return Request{}, ErrUnsupported
	}
	return r, nil
}

func (r *Request) parseQuestions() error {
	if jsoninput.Required(r.frame.Params, "questions", "isBlocking") != nil {
		return ErrProtocol
	}
	var value struct {
		Blocking  bool              `json:"isBlocking"`
		Questions []json.RawMessage `json:"questions"`
	}
	if json.Unmarshal(r.frame.Params, &value) != nil || len(value.Questions) == 0 || len(value.Questions) > 64 {
		return ErrProtocol
	}
	r.blocking = value.Blocking
	seen := map[string]bool{}
	for _, raw := range value.Questions {
		var q Question
		if jsoninput.Required(raw, "id", "header", "question") != nil || json.Unmarshal(raw, &q) != nil ||
			q.ID == "" || len(q.ID) > 512 || !utf8.ValidString(q.ID) || seen[q.ID] {
			return ErrProtocol
		}
		seen[q.ID] = true
		r.questions = append(r.questions, q)
	}
	return nil
}

func (r Request) Questions() []Question {
	result := append([]Question{}, r.questions...)
	for i := range result {
		result[i].Options = append([]QuestionOption{}, result[i].Options...)
	}
	return result
}

func (r Request) Decisions() []json.RawMessage {
	result := make([]json.RawMessage, len(r.decisions))
	for i := range result {
		result[i] = append(json.RawMessage{}, r.decisions[i]...)
	}
	return result
}

func (r Request) Answer(answers map[string][]string) (ResponseReceipt, error) {
	if r.frame.owner == nil || len(r.questions) == 0 || len(answers) != len(r.questions) {
		return ResponseReceipt{}, ErrInvalidAnswer
	}
	total := 0
	values := make(map[string]any, len(answers))
	for _, q := range r.questions {
		answer, ok := answers[q.ID]
		if !ok || answer == nil || len(answer) > 32 {
			return ResponseReceipt{}, ErrInvalidAnswer
		}
		for _, text := range answer {
			total += len(text)
			if !utf8.ValidString(text) || total > maxFrameBytes/2 {
				return ResponseReceipt{}, ErrInvalidAnswer
			}
		}
		values[q.ID] = map[string]any{"answers": answer}
	}
	return r.frame.owner.Respond(r.frame, map[string]any{"answers": values})
}
