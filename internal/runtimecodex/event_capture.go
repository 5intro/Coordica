package runtimecodex

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"coordica/internal/agent/appserver"
	"coordica/internal/runtime"
)

const captureLimit = 32

type eventCapture struct {
	mu          sync.Mutex
	call        runtime.NativeCreate
	native      runtime.NativeSessionID
	queue       []runtime.EventInput
	last, acked int64
	intentional bool
	end         *runtime.SourceEnd
}

func newEventCapture(call runtime.NativeCreate, id runtime.NativeSessionID) *eventCapture {
	return &eventCapture{call: call, native: id}
}
func (c *eventCapture) bind(id runtime.NativeSessionID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.native.Value == "" {
		c.native = id
	}
}
func (c *eventCapture) stopping(intentional bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.intentional = c.intentional || intentional
}

func nativeID(s string) bool {
	return s != "" && len(s) <= 512 && utf8.ValidString(s) && !containsSpace(s)
}
func containsSpace(s string) bool {
	for _, r := range s {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return true
		}
	}
	return false
}

func normalizeFrame(frame appserver.Frame) (runtime.EventInput, bool) {
	var p struct {
		ThreadID  string          `json:"threadId"`
		TurnID    string          `json:"turnId"`
		RequestID json.RawMessage `json:"requestId"`
		Thread    struct {
			ID string `json:"id"`
		} `json:"thread"`
		Turn struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"turn"`
		Delta string `json:"delta"`
		Item  struct {
			Type string `json:"type"`
		} `json:"item"`
		TokenUsage struct {
			Total struct {
				TotalTokens int64 `json:"totalTokens"`
			} `json:"total"`
		} `json:"tokenUsage"`
	}
	if json.Unmarshal(frame.Params, &p) != nil {
		return runtime.EventInput{}, false
	}
	thread := p.ThreadID
	if thread == "" {
		thread = p.Thread.ID
	}
	if !nativeID(thread) {
		return runtime.EventInput{}, false
	}
	input := runtime.EventInput{NativeID: runtime.NativeSessionID{Harness: "codex", Value: thread}, OccurredAt: time.Now().UTC()}
	turn := p.TurnID
	if turn == "" {
		turn = p.Turn.ID
	}
	if turn != "" && !nativeID(turn) {
		return input, false
	}
	input.Fact.NativeTurnID = turn
	switch frame.Method {
	case "thread/started":
		input.Kind, input.Fact.State = "session.updated", "created"
	case "turn/started":
		input.Kind, input.Fact.State = "execution.started", "started"
	case "turn/completed":
		input.Kind = "execution.ended"
		input.Fact.State = p.Turn.Status
		if p.Turn.Status == "failed" {
			input.Kind = "execution.failed"
		} else if p.Turn.Status != "completed" && p.Turn.Status != "interrupted" {
			return input, false
		}
	case "item/agentMessage/delta":
		input.Kind = "output.delta"
		input.Fact.Bytes = int64(len(p.Delta))
	case "item/started", "item/completed":
		input.Kind, input.Fact.State = "session.updated", "observed"
		if p.Item.Type == "commandExecution" || p.Item.Type == "fileChange" || p.Item.Type == "mcpToolCall" {
			input.Kind = "tool.activity"
		}
	case "item/tool/requestUserInput", "item/commandExecution/requestApproval", "item/fileChange/requestApproval":
		input.Kind, input.Fact.State = "request.created", "created"
		p.RequestID = frame.ID
	case "serverRequest/resolved":
		input.Kind, input.Fact.State = "request.resolved", "resolved"
	case "thread/tokenUsage/updated":
		input.Kind = "usage.observed"
		input.Fact.Tokens = p.TokenUsage.Total.TotalTokens
	default:
		return input, false
	}
	if input.Kind == "request.created" || input.Kind == "request.resolved" {
		var id string
		if json.Unmarshal(p.RequestID, &id) == nil {
			if !nativeID(id) {
				return input, false
			}
			input.Fact.NativeRequestID = "s:" + id
		} else {
			var number json.Number
			if json.Unmarshal(p.RequestID, &number) != nil || !nativeID(string(number)) {
				return input, false
			}
			input.Fact.NativeRequestID = "n:" + string(number)
		}
	}
	if len(input.Fact.NativeRequestID) > 512 {
		return input, false
	}
	return input, true
}

// run never takes a lease/DB/UI lock. Overflow/unsupported critical frames stop
// this Peer explicitly; only bounded normalized metadata enters the queue.
func (c *eventCapture) run(peer *appserver.Peer) {
	reason := ""
	gap := false
	for frame := range peer.Events() {
		input, ok := normalizeFrame(frame)
		c.mu.Lock()
		if c.last < math.MaxInt64 {
			c.last++
		} else {
			ok = false
		}
		if reason == "" {
			if !ok {
				reason, gap = "unsupported", true
			} else if len(c.queue) >= captureLimit {
				reason, gap = "backpressure", true
			} else {
				input.SourceSequence = c.last
				input.EventID = fmt.Sprintf("local:%d", c.last)
				c.queue = append(c.queue, input)
			}
		}
		c.mu.Unlock()
		if gap {
			peer.Close()
		} // Close is idempotent and does not require draining Events.
	}
	result, _ := peer.Close()
	c.mu.Lock()
	defer c.mu.Unlock()
	if reason == "" {
		if c.intentional {
			reason = "closed"
		} else {
			reason, gap = "transport_lost", true
		}
	}
	c.end = &runtime.SourceEnd{Reason: reason, Gap: gap, LastObserved: c.last, Stop: runtime.BindingStop{BindingID: c.call.BindingID,
		OwnerID: c.call.OwnerID, NativeID: c.native, TreeStopped: result.TreeStopped}}
}

func (p *Port) capture(call runtime.NativeCreate, id runtime.NativeSessionID) (*eventCapture, error) {
	if call.Config != p.config {
		return nil, runtime.ErrForbidden
	}
	p.mu.Lock()
	l := p.slots[call.BindingID]
	p.mu.Unlock()
	if l == nil {
		return nil, runtime.ErrConflict
	}
	c := l.capture
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.call.BindingID != call.BindingID || c.call.OwnerID != call.OwnerID || c.call.Generation != call.Generation || c.native != id {
		return nil, runtime.ErrConflict
	}
	return c, nil
}
func (p *Port) Captured(ctx context.Context, call runtime.NativeCreate, id runtime.NativeSessionID, limit int) (runtime.SourceStamp, []runtime.EventInput, *runtime.SourceEnd, error) {
	if err := ctx.Err(); err != nil {
		return runtime.SourceStamp{}, nil, nil, err
	}
	if limit < 1 || limit > 32 {
		return runtime.SourceStamp{}, nil, nil, runtime.ErrInvalid
	}
	c, err := p.capture(call, id)
	if err != nil {
		return runtime.SourceStamp{}, nil, nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	count := len(c.queue)
	if count > limit {
		count = limit
	}
	batch := append([]runtime.EventInput(nil), c.queue[:count]...)
	var end *runtime.SourceEnd
	if c.end != nil && len(c.queue) <= limit {
		value := *c.end
		end = &value
	}
	return runtime.SourceStamp{Epoch: c.call.CommandID, Evidence: "local_observed"}, batch, end, nil
}
func (p *Port) ReleaseCaptured(ctx context.Context, call runtime.NativeCreate, id runtime.NativeSessionID, through int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c, err := p.capture(call, id)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if through < c.acked || through > c.last {
		return runtime.ErrConflict
	}
	count := 0
	for count < len(c.queue) && c.queue[count].SourceSequence <= through {
		count++
	}
	c.queue = append([]runtime.EventInput(nil), c.queue[count:]...)
	c.acked = through
	return nil
}
