package runner

import (
	"sync"
	"time"

	"coordica/internal/agent"
	"coordica/internal/domain"
)

// Event collection is passive: no new sessions, interrupting prompts or extra
// filesystem scans. Missing activity describes uncertainty, never task failure.
type progressObserver struct {
	mu       sync.Mutex
	interval time.Duration
	started  time.Time
	counters domain.ProgressObservation
}

func newProgressObserver(run domain.Run, started time.Time) *progressObserver {
	if !domain.ProgressManaged(run) {
		return nil
	}
	seconds := run.ProgressCheckSeconds
	if seconds == 0 {
		seconds = domain.DefaultProgressCheckSeconds
	}
	return &progressObserver{interval: time.Duration(seconds) * time.Second, started: started}
}

func (p *progressObserver) record(event agent.Event) {
	if p == nil {
		return
	}
	activity, ok := agent.LeaderPublicActivity(event)
	if !ok {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	switch activity.Kind {
	case "tool_started":
		p.counters.ToolStarted++
	case "tool_completed":
		p.counters.ToolCompleted++
	case "message", "result_received":
		p.counters.Messages++
	case "plan":
		p.counters.Plans++
	}
}

func (p *progressObserver) snapshot(now time.Time) *domain.ProgressObservation {
	if p == nil || now.Before(p.started.Add(p.interval)) {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	value := p.counters
	value.Sequence = int64(now.Sub(p.started) / p.interval)
	return &value
}
