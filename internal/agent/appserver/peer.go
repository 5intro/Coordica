// Package appserver provides the bounded Codex app-server transport. It does
// not select models, authorize work, run tools, or own Task/Session state.
package appserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"coordica/internal/process"
)

var (
	ErrProtocol       = errors.New("invalid app-server protocol")
	ErrBackpressure   = errors.New("app-server transport capacity exhausted")
	ErrDisconnected   = errors.New("app-server connection ended")
	ErrOutcomeUnknown = errors.New("app-server request outcome is unknown")
	ErrStaleRequest   = errors.New("app-server request is no longer answerable")
)

const (
	maxFrameBytes = 256 << 10
	maxCalls      = 32
	maxNativeIDs  = 1024
	rpcTimeout    = 10 * time.Second
)

// Config is trusted host configuration, never an executable supplied by a task.
// Env must be explicit: the caller excludes center/publication authority while
// preserving the selected harness's official authentication and transport.
type Config struct {
	Executable   string
	Directory    string
	Env          []string
	Experimental bool
}

// ServerInfo contains native initialization facts, not a capability or auth
// attestation. The caller can retain these alongside its selected instance.
type ServerInfo struct {
	UserAgent      string `json:"userAgent"`
	PlatformFamily string `json:"platformFamily"`
	PlatformOS     string `json:"platformOs"`
}

// Frame preserves structured native notifications and requests for the control
// layer. Raw parameters need redaction before public persistence or display.
// Its private connection identity prevents answering a prior process's callback.
type Frame struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
	ID     json.RawMessage `json:"id,omitempty"`
	owner  *Peer
}

type response struct {
	result json.RawMessage
	err    *RPCError
}

// Peer owns one supervised connection. Callers drain Events independently of
// Call/Respond. A slow consumer fails the connection explicitly; it never turns
// a dropped request, lost response, or process exit into execution success.
type Peer struct {
	process       *process.Session
	writes        chan []byte
	events        chan Frame
	failed        chan struct{}
	done          chan struct{}
	mu            sync.Mutex
	nextID        uint64
	ready         bool
	err           error
	waiters       map[string]chan response
	native        map[string]nativeRequest
	invalidScopes map[requestScope]bool
	result        process.Result
	waitErr       error
	info          ServerInfo
	directory     string
	experimental  bool
}

// Open performs only initialize/initialized. It does not create a native thread,
// log in, start a turn, or change native permission defaults.
func Open(ctx context.Context, config Config) (*Peer, error) {
	if !filepath.IsAbs(config.Executable) || !filepath.IsAbs(config.Directory) || config.Env == nil {
		return nil, errors.New("absolute executable/directory and explicit execution environment are required")
	}
	directory, err := filepath.EvalSymlinks(config.Directory)
	if err != nil {
		return nil, err
	}
	s, err := process.Start(ctx, process.Spec{Executable: config.Executable, Args: []string{"app-server"},
		Dir: config.Directory, Env: config.Env, MaxLineBytes: maxFrameBytes})
	if err != nil {
		return nil, err
	}
	p := &Peer{process: s, writes: make(chan []byte, 8), events: make(chan Frame, 64), failed: make(chan struct{}),
		done: make(chan struct{}), waiters: map[string]chan response{}, native: map[string]nativeRequest{},
		directory: filepath.Clean(directory), experimental: config.Experimental, invalidScopes: map[requestScope]bool{}}
	go p.writeLoop()
	go p.readLoop()
	params := map[string]any{"clientInfo": map[string]string{"name": "coordica", "version": "0.2.5-dev"},
		"capabilities": map[string]bool{"experimentalApi": config.Experimental}}
	var result ServerInfo
	err = p.call(ctx, "initialize", params, &result)
	if err == nil && result.UserAgent == "" {
		err = ErrProtocol
	}
	if err == nil {
		p.mu.Lock()
		err = p.enqueueLocked(map[string]string{"method": "initialized"})
		p.ready = err == nil
		p.info = result
		p.mu.Unlock()
	}
	if err != nil {
		status, stopErr := p.Close()
		if !status.TreeStopped {
			err = errors.Join(err, stopErr, process.ErrTreeNotStopped)
		}
		return nil, err
	}
	return p, nil
}

func (p *Peer) Events() <-chan Frame { return p.events }

func (p *Peer) Info() ServerInfo {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.info
}

// Call invokes one explicit native method. A timeout or disconnect after queue
// acceptance is uncertain and is never retried. Task/binding/turn authorization
// and idempotent command receipts belong to the calling control layer.
func (p *Peer) Call(ctx context.Context, method string, params, result any) error {
	p.mu.Lock()
	ready := p.ready
	p.mu.Unlock()
	if !ready {
		return ErrDisconnected
	}
	return p.call(ctx, method, params, result)
}

func (p *Peer) call(ctx context.Context, method string, params, result any) error {
	ctx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validMethod(method) {
		return ErrProtocol
	}
	p.mu.Lock()
	if p.err != nil {
		p.mu.Unlock()
		return ErrDisconnected
	}
	if len(p.waiters) >= maxCalls || p.nextID == ^uint64(0) {
		p.mu.Unlock()
		return ErrBackpressure
	}
	p.nextID++
	id := fmt.Sprintf("coordica:%d", p.nextID)
	reply := make(chan response, 1)
	p.waiters["s:"+id] = reply
	err := p.enqueueLocked(map[string]any{"id": id, "method": method, "params": params})
	if err != nil {
		delete(p.waiters, "s:"+id)
	}
	p.mu.Unlock()
	if err != nil {
		return err
	}
	select {
	case value := <-reply:
		return applyResponse(value, result)
	case <-ctx.Done():
		select {
		case value := <-reply:
			return applyResponse(value, result)
		default:
		}
		p.fail(ctx.Err())
		return errors.Join(ErrOutcomeUnknown, ctx.Err())
	case <-p.failed:
		select {
		case value := <-reply:
			return applyResponse(value, result)
		default:
		}
		return errors.Join(ErrOutcomeUnknown, p.Err())
	}
}

func applyResponse(value response, result any) error {
	if value.err != nil {
		return value.err
	}
	if result != nil && json.Unmarshal(value.result, result) != nil {
		return ErrProtocol
	}
	return nil
}

// Err reports the connection failure, not a Task or Turn terminal status.
func (p *Peer) Err() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.err
}

// Close is idempotent and returns the actual supervised process-tree evidence.
// It releases this transport, not the logical Task or stored native history.
// Controlled cancellation may appear in the error even when TreeStopped is true.
func (p *Peer) Close() (process.Result, error) {
	p.fail(context.Canceled)
	<-p.done
	return p.result, p.waitErr
}

func (p *Peer) fail(err error) {
	p.mu.Lock()
	if p.err == nil {
		p.err = err
		close(p.failed)
		p.process.Stop()
	}
	p.mu.Unlock()
}

func (p *Peer) enqueueLocked(value any) error {
	if p.err != nil {
		return ErrDisconnected
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(raw) > maxFrameBytes {
		return ErrBackpressure
	}
	select {
	case p.writes <- append(raw, '\n'):
		return nil
	default:
		return ErrBackpressure
	}
}

func (p *Peer) writeLoop() {
	for {
		select {
		case <-p.failed:
			return
		case raw := <-p.writes:
			select {
			case <-p.failed:
				return
			default:
			}
			if err := p.process.Write(raw); err != nil {
				p.fail(err)
				return
			}
		}
	}
}

func (p *Peer) readLoop() {
	defer close(p.done)
	defer close(p.events)
	for output := range p.process.Output() {
		if output.Stream != "stdout" || p.Err() != nil {
			continue
		}
		if err := p.receive(output.Line); err != nil {
			p.fail(err)
		}
	}
	p.result, p.waitErr = p.process.Wait()
	p.fail(errors.Join(ErrDisconnected, p.waitErr))
}
