//go:build !windows

package process

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCancellationEscalatesToKILL(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, err := Start(ctx, Spec{Executable: "/bin/sh", Args: []string{"-c", "trap '' TERM; echo ready; while :; do :; done"}, GracePeriod: 50 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	for event := range s.Output() {
		if string(event.Line) == "ready" {
			cancel()
		}
	}
	r, err := s.Wait()
	if !errors.Is(err, context.Canceled) || !r.TreeStopped || r.ExitCode != -1 {
		t.Fatalf("result=%+v err=%v", r, err)
	}
}
