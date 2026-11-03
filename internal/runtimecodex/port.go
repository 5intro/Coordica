// Package runtimecodex composes the experimental ledger port with the Codex
// protocol adapter. Neither package owns the other's scheduling or authority.
package runtimecodex

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"time"

	"coordica/internal/agent/appserver"
	"coordica/internal/runtime"
)

// Port is trusted host wiring. Constructing it performs no native I/O. The host
// lifetime owns peers independently of individual bounded RPC contexts.
type Port struct {
	lifetime context.Context
	config   runtime.BindingConfig
	native   appserver.Config
	mu       sync.Mutex
	slots    map[string]*lease
}

type lease struct {
	mu            sync.Mutex
	call          runtime.NativeCreate
	id            runtime.NativeSessionID
	peer          *appserver.Peer
	thread        appserver.Thread
	stopped       bool
	openingFailed bool
	capture       *eventCapture
}

var _ runtime.SessionPort = (*Port)(nil)

func New(lifetime context.Context, config runtime.BindingConfig, native appserver.Config) (*Port, error) {
	if lifetime == nil || config.Target.Harness != "codex" || !filepath.IsAbs(native.Executable) ||
		!filepath.IsAbs(native.Directory) || native.Env == nil {
		return nil, runtime.ErrInvalid
	}
	native.Env = append([]string{}, native.Env...)
	return &Port{lifetime: lifetime, config: config, native: native, slots: map[string]*lease{}}, nil
}

func lockLease(ctx context.Context, l *lease) error {
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for !l.mu.TryLock() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
	return nil
}

func (p *Port) reserve(ctx context.Context, call runtime.NativeCreate, id runtime.NativeSessionID, resume bool) (*lease, error) {
	if call.Config != p.config || call.BindingID == "" || call.OwnerID == "" || call.CommandID == "" || call.Generation <= 0 ||
		resume && (id.Harness != "codex" || id.Value == "") {
		return nil, runtime.ErrForbidden
	}
	p.mu.Lock()
	old := p.slots[call.BindingID]
	p.mu.Unlock()
	if old != nil {
		if err := lockLease(ctx, old); err != nil {
			return nil, err
		}
		defer old.mu.Unlock()
		allowed := resume && old.stopped && old.id == id && call.Generation > old.call.Generation
		if !allowed {
			return nil, runtime.ErrConflict
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.slots[call.BindingID] != old {
		return nil, runtime.ErrConflict
	}
	l := &lease{call: call, id: id, capture: newEventCapture(call, id)}
	// Hold this lease until opening completes. Stop cannot prove an unstarted
	// process while the opening goroutine is about to allocate it.
	l.mu.Lock()
	p.slots[call.BindingID] = l
	return l, nil
}

func classify(err error) string {
	if err == nil {
		return "confirmed"
	}
	var rpc *appserver.RPCError
	if errors.As(err, &rpc) {
		return "rejected"
	}
	if !errors.Is(err, appserver.ErrOutcomeUnknown) &&
		(errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, appserver.ErrBackpressure)) {
		return "not_sent"
	}
	return "unknown"
}

func (p *Port) open(ctx context.Context, call runtime.NativeCreate, id runtime.NativeSessionID, resume bool) (runtime.NativeCreation, error) {
	if err := ctx.Err(); err != nil {
		return runtime.NativeCreation{Outcome: "not_sent"}, err
	}
	l, err := p.reserve(ctx, call, id, resume)
	if err != nil {
		return runtime.NativeCreation{Outcome: "not_sent"}, err
	}
	defer l.mu.Unlock()
	peer, err := appserver.OpenWithLifetime(p.lifetime, ctx, p.native)
	if err != nil {
		l.openingFailed = true
		return runtime.NativeCreation{Outcome: "unknown"}, err
	}
	l.peer = peer
	go l.capture.run(peer)
	options := appserver.ThreadOptions{Model: call.Config.Target.Model, ApprovalPolicy: call.Config.ApprovalPolicy, Sandbox: call.Config.Sandbox}
	var configuration appserver.Configuration
	if resume {
		l.thread, configuration, err = appserver.ResumeThread(ctx, peer, id.Value, options)
	} else {
		l.thread, configuration, err = appserver.StartThread(ctx, peer, options)
	}
	observed := runtime.NativeCreation{Outcome: classify(err)}
	observed.Configuration, _ = json.Marshal(configuration)
	if configuration.NativeThreadID != "" {
		observed.ID = runtime.NativeSessionID{Harness: "codex", Value: configuration.NativeThreadID}
		if !resume {
			l.id = observed.ID
		}
	}
	if err == nil {
		observed.Effective = call.Config
		l.id = observed.ID
	} else if errors.Is(err, appserver.ErrConfiguration) {
		// Native allocated an identity but its selected fields differ. Keep the
		// observation; never label this as a no-side-effect rejection.
		observed.Outcome = "unknown"
	}
	l.capture.bind(observed.ID)
	if err == nil && peer.Err() != nil {
		observed.Outcome = "unknown"
		err = appserver.ErrOutcomeUnknown
	}
	return observed, err
}

func (p *Port) Create(ctx context.Context, call runtime.NativeCreate) (runtime.NativeCreation, error) {
	return p.open(ctx, call, runtime.NativeSessionID{}, false)
}

func (p *Port) Resume(ctx context.Context, call runtime.NativeCreate, id runtime.NativeSessionID) (runtime.NativeCreation, error) {
	return p.open(ctx, call, id, true)
}

func (p *Port) owned(ctx context.Context, call runtime.NativeCreate, id runtime.NativeSessionID) (*lease, error) {
	if call.Config != p.config {
		return nil, runtime.ErrForbidden
	}
	p.mu.Lock()
	l := p.slots[call.BindingID]
	p.mu.Unlock()
	if l == nil {
		return nil, runtime.ErrStopUnconfirmed
	}
	if err := lockLease(ctx, l); err != nil {
		return nil, err
	}
	if l.call.BindingID != call.BindingID || l.call.OwnerID != call.OwnerID || l.call.Generation != call.Generation || l.id != id {
		l.mu.Unlock()
		return nil, runtime.ErrConflict
	}
	return l, nil
}

func (p *Port) Read(ctx context.Context, call runtime.NativeCreate, id runtime.NativeSessionID, turns bool) (runtime.NativeSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return runtime.NativeSnapshot{Outcome: "not_sent"}, err
	}
	l, err := p.owned(ctx, call, id)
	if err != nil {
		return runtime.NativeSnapshot{Outcome: "not_sent"}, err
	}
	defer l.mu.Unlock()
	if l.peer == nil || l.stopped {
		return runtime.NativeSnapshot{Outcome: "unknown"}, appserver.ErrDisconnected
	}
	raw, err := l.thread.Read(ctx, turns)
	return runtime.NativeSnapshot{Outcome: classify(err), History: raw}, err
}

func (p *Port) Stop(ctx context.Context, call runtime.NativeCreate, id runtime.NativeSessionID) (runtime.BindingStop, error) {
	l, err := p.owned(ctx, call, id)
	if err != nil {
		return runtime.BindingStop{}, err
	}
	defer l.mu.Unlock()
	proof := runtime.BindingStop{BindingID: call.BindingID, OwnerID: call.OwnerID, NativeID: id}
	if l.peer == nil || l.openingFailed {
		return proof, runtime.ErrStopUnconfirmed
	}
	l.capture.stopping(l.peer.Err() == nil)
	result, err := l.peer.Close()
	l.stopped = result.TreeStopped
	proof.TreeStopped = result.TreeStopped
	if result.TreeStopped {
		err = nil
	}
	return proof, err
}
