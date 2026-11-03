package acpclient

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"time"

	"coordica/internal/process"
)

type initializeReply struct {
	info Info
	err  error
}

// Peer owns one supervised, initialized connection. There is intentionally no
// arbitrary Call, authentication or callback-response API.
type Peer struct {
	process                 *process.Session
	mu                      sync.Mutex
	pending                 bool
	err                     error
	info                    Info
	reply                   chan initializeReply
	failed, done, writeDone chan struct{}
	result                  process.Result
	waitErr                 error
	directory               string
	sessionClaimed          bool
	sessionPending          bool
	sessionID               string
	sessionReply            chan sessionReply
	sessionWriteDone        chan struct{}
	sessionReady            bool
	updates                 *updateCapture
	prompt                  *pendingPrompt
	lastPromptID            uint64
}

func Open(ctx context.Context, config Config) (*Peer, error) {
	return OpenWithLifetime(ctx, ctx, config)
}

// OpenWithLifetime only initializes ACP v1. After successful initialization,
// cancelling operation does not end the host lifetime. An initialization
// cancelled after send acceptance is uncertain and is never retried.
func OpenWithLifetime(lifetime, operation context.Context, config Config) (*Peer, error) {
	if lifetime == nil || operation == nil || !filepath.IsAbs(config.Executable) || !filepath.IsAbs(config.Directory) || config.Env == nil || config.Args == nil {
		return nil, errors.New("absolute executable/directory and explicit argv/environment are required")
	}
	if err := lifetime.Err(); err != nil {
		return nil, err
	}
	if err := operation.Err(); err != nil {
		return nil, err
	}
	directory, err := filepath.EvalSymlinks(config.Directory)
	if err != nil {
		return nil, err
	}
	capture, err := newUpdateCapture()
	if err != nil {
		return nil, err
	}
	s, err := process.Start(lifetime, process.Spec{Executable: config.Executable, Args: append([]string{}, config.Args...), Dir: directory,
		Env: append([]string{}, config.Env...), MaxLineBytes: maxFrameBytes})
	if err != nil {
		return nil, err
	}
	p := &Peer{process: s, directory: directory, updates: capture, pending: true, reply: make(chan initializeReply, 1), failed: make(chan struct{}), done: make(chan struct{}), writeDone: make(chan struct{})}
	go p.readLoop()
	request, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{
		"protocolVersion": 1, "clientInfo": Implementation{Name: "coordica", Version: "0.2.5-dev"},
		"clientCapabilities": map[string]any{"fs": map[string]bool{"readTextFile": false, "writeTextFile": false}, "terminal": false}}})
	go func() {
		defer close(p.writeDone)
		if err := s.Write(append(request, '\n')); err != nil {
			p.fail(errors.Join(ErrOutcomeUnknown, err))
		}
	}()
	ctx, cancel := context.WithTimeout(operation, 10*time.Second)
	defer cancel()
	select {
	case response := <-p.reply:
		err = response.err
		if err == nil {
			err = p.Err()
		}
	case <-p.failed:
		err = p.initializationFailure(p.Err())
	case <-ctx.Done():
		err = p.initializationFailure(ctx.Err())
	}
	if err != nil {
		status, stopErr := p.Close()
		if !status.TreeStopped {
			err = errors.Join(err, stopErr, process.ErrTreeNotStopped)
		}
		return nil, &InitializationError{Cause: err, Process: status}
	}
	return p, nil
}

func (p *Peer) initializationFailure(cause error) error {
	// A correlated explicit rejection already received remains a rejection
	// even when process exit or cancellation wins the selecting goroutine.
	select {
	case response := <-p.reply:
		if response.err != nil {
			return response.err
		}
	default:
	}
	return errors.Join(ErrOutcomeUnknown, cause)
}

// Info returns a defensive copy of historical initialization declarations.
func (p *Peer) Info() Info {
	p.mu.Lock()
	defer p.mu.Unlock()
	info := p.info
	if info.AgentInfo != nil {
		copy := *info.AgentInfo
		info.AgentInfo = &copy
	}
	info.AuthMethods = append([]AuthMethod{}, info.AuthMethods...)
	return info
}

func (p *Peer) Err() error            { p.mu.Lock(); defer p.mu.Unlock(); return p.err }
func (p *Peer) Done() <-chan struct{} { return p.done }

func (p *Peer) Close() (process.Result, error) {
	p.updates.stopping()
	p.fail(context.Canceled)
	<-p.done
	<-p.writeDone
	p.mu.Lock()
	sessionWriter := p.sessionWriteDone
	p.mu.Unlock()
	if sessionWriter != nil {
		<-sessionWriter
	}
	p.mu.Lock()
	prompt := p.prompt
	p.mu.Unlock()
	if prompt != nil {
		<-prompt.writeDone
	}
	return p.result, p.waitErr
}

func (p *Peer) fail(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err == nil {
		p.err = err
		close(p.failed)
		p.process.Stop()
	}
}

func (p *Peer) readLoop() {
	defer close(p.done)
	for output := range p.process.Output() {
		if output.Stream != "stdout" || p.Err() != nil {
			continue
		}
		if err := p.receive(output.Line); err != nil {
			p.updates.fault("protocol_error")
			p.fail(errors.Join(ErrOutcomeUnknown, err))
		}
	}
	p.result, p.waitErr = p.process.Wait()
	p.updates.finish(p.result)
	p.fail(errors.Join(ErrDisconnected, p.waitErr))
}
