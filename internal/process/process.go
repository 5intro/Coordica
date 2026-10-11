// Package process supervises a command and its ordinary descendants. It does
// not turn a private user's development environment into a security sandbox.
package process

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

var ErrTreeNotStopped = errors.New("cannot confirm managed process tree stopped")
var ErrOutputLimit = errors.New("managed process output limit exceeded")

type Spec struct {
	Executable   string
	Args         []string
	Dir          string
	Env          []string // nil inherits the parent environment; an empty slice does not.
	GracePeriod  time.Duration
	DrainTimeout time.Duration
	MaxLineBytes int
	RawOutput    bool // preserves binary output, including NULs and final newlines.
}

type Output struct {
	Stream string
	Line   []byte
}

type Result struct {
	ExitCode    int
	Canceled    bool
	TreeStopped bool
}

type Session struct {
	cmd             *exec.Cmd
	in, out, errOut *os.File
	output          chan Output
	stop            chan struct{}
	done            chan struct{}
	once            sync.Once
	writeMu         sync.Mutex
	result          Result
	err             error
}

// Start starts a new supervised command. Callers must consume Output until it
// closes, or cancel the context. Lines omit their final newline unless RawOutput
// is set, in which case Output.Line contains unmodified byte chunks. Stdout and
// stderr are independently ordered; neither is interpreted as terminal text.
func Start(ctx context.Context, spec Spec) (*Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if spec.Executable == "" {
		return nil, errors.New("executable is required")
	}
	if spec.GracePeriod <= 0 {
		spec.GracePeriod = 750 * time.Millisecond
	}
	if spec.DrainTimeout <= 0 {
		spec.DrainTimeout = 2 * time.Second
	}
	if spec.MaxLineBytes <= 0 {
		spec.MaxLineBytes = 4 << 20
	}
	cmd, err := commandFor(spec)
	if err != nil {
		return nil, err
	}
	cmd.Dir, cmd.Env = spec.Dir, spec.Env
	inR, inW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		inR.Close()
		inW.Close()
		return nil, err
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		inR.Close()
		inW.Close()
		outR.Close()
		outW.Close()
		return nil, err
	}
	closeAll := func() {
		inR.Close()
		inW.Close()
		outR.Close()
		outW.Close()
		errR.Close()
		errW.Close()
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = inR, outW, errW
	guard, err := prepare(cmd)
	if err != nil {
		closeAll()
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		guard.close()
		closeAll()
		return nil, err
	}
	// Windows starts suspended. Allocation failure kills the never-run child;
	// there is no fallback to an unconfined, already running process.
	if err = guard.started(cmd.Process.Pid); err != nil {
		cmd.Process.Kill()
		guard.close()
		closeAll()
		cmd.Wait()
		return nil, err
	}
	inR.Close()
	outW.Close()
	errW.Close()
	s := &Session{cmd: cmd, in: inW, out: outR, errOut: errR, output: make(chan Output, 64), stop: make(chan struct{}), done: make(chan struct{})}
	go s.supervise(ctx, spec, guard)
	return s, nil
}

func (s *Session) PID() int              { return s.cmd.Process.Pid }
func (s *Session) Output() <-chan Output { return s.output }
func (s *Session) Write(data []byte) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_, err := s.in.Write(data)
	return err
}
func (s *Session) CloseInput() error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.in.Close()
}

// Stop requests supervised cancellation. It is safe to call more than once.
func (s *Session) Stop() { s.once.Do(func() { close(s.stop) }) }

// Wait drains output and reaps the direct child, or reports that a bounded
// shutdown could not confirm it. ErrTreeNotStopped must not be treated as idle.
func (s *Session) Wait() (Result, error) { <-s.done; return s.result, s.err }

func (s *Session) supervise(ctx context.Context, spec Spec, guard *platformGuard) {
	defer close(s.done)
	defer guard.close()
	defer s.in.Close()
	readDone := make(chan error, 2)
	discard := make(chan struct{})
	read := func(stream string, f *os.File) {
		if spec.RawOutput {
			buffer := make([]byte, 32<<10)
			for {
				n, err := f.Read(buffer)
				if n > 0 {
					chunk := append([]byte(nil), buffer[:n]...)
					select {
					case s.output <- Output{Stream: stream, Line: chunk}:
					case <-discard:
					}
				}
				if err != nil {
					if errors.Is(err, io.EOF) {
						err = nil
					} else {
						s.Stop()
					}
					readDone <- err
					return
				}
			}
		}
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 4096), spec.MaxLineBytes)
		for scanner.Scan() {
			line := append([]byte(nil), scanner.Bytes()...)
			select {
			case s.output <- Output{Stream: stream, Line: line}:
			case <-discard:
			}
		}
		if scanner.Err() != nil {
			s.Stop()
		}
		readDone <- scanner.Err()
	}
	go read("stdout", s.out)
	go read("stderr", s.errOut)
	type childExit struct {
		err  error
		code int
	}
	waited := make(chan childExit, 1)
	go func() {
		err := s.cmd.Wait()
		code := -1
		if s.cmd.ProcessState != nil {
			code = s.cmd.ProcessState.ExitCode()
		}
		waited <- childExit{err: err, code: code}
	}()
	var waitErr error
	var shutdownErr error
	reaped := false
	s.result.ExitCode = -1
	receive := func(exit childExit) { waitErr = exit.err; s.result.ExitCode = exit.code; reaped = true }
	select {
	case exit := <-waited:
		receive(exit)
	case <-ctx.Done():
		s.result.Canceled = true
	case <-s.stop:
		s.result.Canceled = true
	}
	if s.result.Canceled {
		// Readers may discard during cancellation so a slow consumer cannot
		// prevent the supervisor from terminating or reaping the process.
		close(discard)
		shutdownErr = guard.terminate()
		timer := time.NewTimer(spec.GracePeriod)
		select {
		case exit := <-waited:
			receive(exit)
			timer.Stop()
		case <-timer.C:
			shutdownErr = errors.Join(shutdownErr, guard.kill())
			reapTimer := time.NewTimer(spec.DrainTimeout)
			select {
			case exit := <-waited:
				receive(exit)
				reapTimer.Stop()
			case <-reapTimer.C:
				shutdownErr = errors.Join(shutdownErr, errors.New("direct child did not exit before shutdown deadline"))
			}
		}
	}
	// A successful parent may still leave children holding a pipe open.
	// Cleanup runs on both normal exit and cancellation.
	stopped, cleanupErr := guard.cleanup(spec.GracePeriod)
	s.result.TreeStopped = stopped && reaped
	if !s.result.TreeStopped {
		cleanupErr = errors.Join(cleanupErr, ErrTreeNotStopped)
	}
	drainTimer := time.NewTimer(spec.DrainTimeout)
	var readErr error
	for remaining := 2; remaining > 0; {
		select {
		case err := <-readDone:
			if err != nil && !errors.Is(err, os.ErrClosed) {
				readErr = errors.Join(readErr, err)
			}
			remaining--
		case <-drainTimer.C:
			s.out.Close()
			s.errOut.Close()
			if !s.result.Canceled {
				close(discard)
			}
			readErr = errors.Join(readErr, errors.New("output drain deadline exceeded"))
		}
	}
	drainTimer.Stop()
	s.out.Close()
	s.errOut.Close()
	close(s.output)
	if s.result.Canceled {
		cancelErr := ctx.Err()
		if cancelErr == nil {
			cancelErr = context.Canceled
		}
		waitErr = errors.Join(waitErr, cancelErr)
	}
	s.err = errors.Join(waitErr, shutdownErr, cleanupErr, readErr)
	if readErr != nil {
		s.err = fmt.Errorf("managed process output: %w", s.err)
	}
}

// CollectRaw collects binary stdout and stderr independently without line
// parsing. Limits bound retained bytes; exceeding either stops the same managed
// process tree and preserves its shutdown evidence in the returned error.
func CollectRaw(ctx context.Context, spec Spec, stdoutLimit, stderrLimit int) ([]byte, []byte, Result, error) {
	if stdoutLimit < 0 || stderrLimit < 0 {
		return nil, nil, Result{ExitCode: -1}, errors.New("output limits cannot be negative")
	}
	spec.RawOutput = true
	s, err := Start(ctx, spec)
	if err != nil {
		return nil, nil, Result{ExitCode: -1}, err
	}
	s.CloseInput()
	var stdout, stderr []byte
	tooLarge := false
	for event := range s.Output() {
		destination, limit := &stdout, stdoutLimit
		if event.Stream == "stderr" {
			destination, limit = &stderr, stderrLimit
		}
		remaining := limit - len(*destination)
		chunk := event.Line
		if len(chunk) > remaining {
			tooLarge = true
			chunk = chunk[:remaining]
			s.Stop()
		}
		*destination = append(*destination, chunk...)
	}
	result, err := s.Wait()
	if tooLarge {
		err = errors.Join(err, ErrOutputLimit)
	}
	return stdout, stderr, result, err
}

// Collect is intended for short preflight commands. The supplied context still
// limits the whole process tree, and excessive output is explicitly rejected.
func Collect(ctx context.Context, spec Spec, limit int) ([]byte, Result, error) {
	s, err := Start(ctx, spec)
	if err != nil {
		return nil, Result{ExitCode: -1}, err
	}
	s.CloseInput()
	var output []byte
	var tooLarge bool
	for event := range s.Output() {
		if event.Stream != "stdout" {
			continue
		}
		if len(output)+len(event.Line)+1 > limit {
			tooLarge = true
			s.Stop()
			continue
		}
		output = append(output, event.Line...)
		output = append(output, '\n')
	}
	result, err := s.Wait()
	if tooLarge {
		err = errors.Join(err, errors.New("command output limit exceeded"))
	}
	return output, result, err
}
