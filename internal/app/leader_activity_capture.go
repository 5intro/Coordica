package app

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"coordica/internal/domain"
	"coordica/internal/store"
)

type leaderActivityContext struct{}
type activityCapture struct {
	mu      sync.Mutex
	closed  bool
	service *Service
	ref     domain.LeaderExecutionRef
	queue   chan domain.LeaderActivityEvent
	done    chan struct{}
	dropped atomic.Int64
	seen    atomic.Int64
}

// Only trusted adapters receive this in-process callback. There is no public
// event-write endpoint and raw protocol payloads never enter persistence.
func ReportLeaderActivity(ctx context.Context, signal domain.LeaderActivitySignal) {
	if report, ok := ctx.Value(leaderActivityContext{}).(func(domain.LeaderActivitySignal)); ok {
		report(signal)
	}
}

func executeObservedLeader(ctx context.Context, service *Service, ref domain.LeaderExecutionRef,
	executor PlanningExecutor, dir, prompt string, onStarted func(int) error,
) (PlanningOutcome, error) {
	capture := &activityCapture{service: service, ref: ref, queue: make(chan domain.LeaderActivityEvent, 64), done: make(chan struct{})}
	go capture.persist()
	defer func() {
		capture.mu.Lock()
		capture.closed = true
		close(capture.queue)
		capture.mu.Unlock()
		select {
		case <-capture.done:
		case <-time.After(250 * time.Millisecond): // Display failure never delays a process-stop receipt indefinitely.
		}
	}()
	return executor.Execute(context.WithValue(ctx, leaderActivityContext{}, capture.report), dir, prompt, onStarted)
}

func (c *activityCapture) report(signal domain.LeaderActivitySignal) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	if !leaderActivityKind(signal.Kind) || signal.Key == "" {
		return
	}
	if c.seen.Add(1) > 512 {
		c.dropped.Add(1)
		return
	}
	text := redacted(signal.Text)
	if len(text) > domain.MaxLeaderEventText {
		text = text[:domain.MaxLeaderEventText]
		for !utf8.ValidString(text) {
			text = text[:len(text)-1]
		}
	}
	event := domain.LeaderActivityEvent{Execution: c.ref, Key: Hash(signal.Key), Kind: signal.Kind, Text: text, Time: c.service.Now()}
	select {
	case c.queue <- event:
	default:
		c.dropped.Add(1)
	}
}

func leaderActivityKind(kind string) bool {
	switch kind {
	case "started", "message", "tool_started", "tool_completed", "plan", "result_received", "turn_finished", "turn_failed", "gap":
		return true
	}
	return false
}

func (c *activityCapture) persist() {
	defer close(c.done)
	for first := range c.queue {
		batch := []domain.LeaderActivityEvent{first}
	drain:
		for len(batch) < 32 {
			select {
			case next, ok := <-c.queue:
				if !ok {
					break drain
				}
				batch = append(batch, next)
			default:
				break drain
			}
		}
		if c.dropped.Swap(0) > 0 {
			batch = append(batch, domain.LeaderActivityEvent{Execution: c.ref, Key: ID("gap_"), Kind: "gap",
				Text: "部分活动因输出过多已省略", Time: c.service.Now()})
		}
		c.save(batch)
	}
	if c.dropped.Swap(0) > 0 {
		c.save([]domain.LeaderActivityEvent{{Execution: c.ref, Key: ID("gap_"), Kind: "gap",
			Text: "部分活动因输出过多已省略", Time: c.service.Now()}})
	}
}

func (c *activityCapture) save(batch []domain.LeaderActivityEvent) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_ = c.service.DB.Update(ctx, func(tx *store.Tx) error {
		if err := activityBindingCurrent(tx, c.ref); err != nil {
			return err
		}
		return store.AppendLeaderActivity(tx, batch)
	})
}

func activityBindingCurrent(tx *store.Tx, ref domain.LeaderExecutionRef) error {
	if ref.Kind == "initialization" {
		current, err := initialization(tx)
		if err != nil {
			return err
		}
		if current.OperationID != ref.ID || current.Version != ref.InputVersion {
			return domain.Fail("stale_run", "initialization display input changed")
		}
		var operation domain.InitializationOperation
		if err := tx.Get("initialization_operation", ref.ID, &operation); err != nil {
			return err
		}
		if operation.State == "running" && !operation.Settled && operation.Generation == ref.Generation &&
			operation.InitializationVersion == ref.InputVersion {
			return nil
		}
	} else if ref.Kind == "planning" {
		var run domain.PlanningRun
		if err := tx.Get("planning_run", ref.ID, &run); err != nil {
			return err
		}
		if run.State == "running" && !run.Settled && run.Generation == ref.Generation && run.DocumentVersion == ref.InputVersion {
			return nil
		}
	}
	return domain.Fail("stale_run", "display event belongs to a stopped or superseded Leader execution")
}
