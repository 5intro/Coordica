package process

import (
	"context"
	"errors"
)

// CollectRawInput writes bounded caller-owned input while draining output.
// EOF, cancellation, output limits and process-tree evidence use the same
// supervised lifecycle as CollectRaw; no interactive terminal is involved.
func CollectRawInput(ctx context.Context, spec Spec, input []byte, stdoutLimit, stderrLimit int) ([]byte, []byte, Result, error) {
	return collectRawInput(ctx, spec, input, stdoutLimit, stderrLimit)
}

func collectRawInput(ctx context.Context, spec Spec, input []byte, stdoutLimit, stderrLimit int) ([]byte, []byte, Result, error) {
	if stdoutLimit < 0 || stderrLimit < 0 {
		return nil, nil, Result{ExitCode: -1}, errors.New("output limits cannot be negative")
	}
	spec.RawOutput = true
	s, err := Start(ctx, spec)
	if err != nil {
		return nil, nil, Result{ExitCode: -1}, err
	}
	sent := make(chan error, 1)
	if len(input) == 0 {
		s.CloseInput()
		sent <- nil
	} else {
		go func() {
			err := s.Write(input)
			sent <- errors.Join(err, s.CloseInput())
		}()
	}
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
	err = errors.Join(err, <-sent)
	if tooLarge {
		err = errors.Join(err, ErrOutputLimit)
	}
	return stdout, stderr, result, err
}
