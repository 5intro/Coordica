package agent

import (
	"context"
	"encoding/json"
	"errors"

	"coordica/internal/process"
)

// Both bidirectional protocols use this bounded, asynchronous input path.
// A peer that stops reading cannot block the receive/cancellation timer.
type rpcProcess struct {
	process     *process.Session
	writes      chan []byte
	writeErrors chan error
	stopWriter  context.CancelFunc
}

func startRPC(def Definition, req Request, onEvent func(Event)) (*rpcProcess, error) {
	return startRPCSpec(process.Spec{Executable: req.Executable, Args: launchArgs(def, req), Dir: req.WorkDir, Env: executionEnv(req)}, onEvent)
}

func startRPCSpec(spec process.Spec, onEvent func(Event)) (*rpcProcess, error) {
	ctx, cancel := context.WithCancel(context.Background())
	s, err := process.Start(ctx, spec)
	if err != nil {
		cancel()
		return nil, err
	}
	r := &rpcProcess{process: s, writes: make(chan []byte, 8), writeErrors: make(chan error, 1), stopWriter: cancel}
	if onEvent != nil {
		emit(onEvent, "process.started", map[string]int{"pid": s.PID()})
	}
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case data := <-r.writes:
				var err error
				if data == nil {
					err = s.CloseInput()
				} else {
					err = s.Write(data)
				}
				if err != nil {
					r.writeErrors <- err
					return
				}
				if data == nil {
					return
				}
			}
		}
	}()
	return r, nil
}

func (r *rpcProcess) send(value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	select {
	case r.writes <- append(data, '\n'):
		return nil
	default:
		return errors.New("protocol input queue exhausted")
	}
}

func (r *rpcProcess) closeInput() error {
	select {
	case r.writes <- nil:
		return nil
	default:
		return errors.New("protocol input queue exhausted")
	}
}

// A successful protocol terminal is still followed by confirmed shutdown.
// Graceful EOF is preferred; if the server ignores it, its process tree must
// be stopped. Controlled teardown's exit code is not an agent task outcome.
func rpcFinished(status process.Result, waitErr error, teardown bool) error {
	if teardown && status.TreeStopped {
		return withoutControlledCancellation(waitErr)
	}
	return waitErr
}

func withoutControlledCancellation(err error) error {
	if err == nil || err == context.Canceled {
		return nil
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		var rest []error
		for _, child := range joined.Unwrap() {
			rest = append(rest, withoutControlledCancellation(child))
		}
		return errors.Join(rest...)
	}
	return err
}
