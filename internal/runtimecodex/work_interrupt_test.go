package runtimecodex

import (
	"context"
	"errors"
	"os"
	"testing"

	"coordica/internal/agent/appserver"
	"coordica/internal/runtime"
)

func TestInterruptWorkNativeACKAndFailuresNeverStartOrRelease(t *testing.T) {
	for _, scenario := range []string{"ack", "rejected", "disconnect", "invalid", "block"} {
		t.Run(scenario, func(t *testing.T) {
			f := setup(t, "work-interrupt-"+scenario)
			b := produce(t, f)
			w := prepareWork(t, f, b, "work", "explicit-interrupt-target")
			w, err := f.producer.ProduceWork(t.Context(), f.cred, w.ID)
			if err != nil {
				t.Fatal(err)
			}
			request := runtime.InterruptWorkRequest{Key: "interrupt", WorkID: w.ID, Generation: w.Generation, ExpectedVersion: w.Version}
			var command runtime.WorkInterrupt
			if scenario == "block" {
				ctx, cancel := context.WithCancel(t.Context())
				done := make(chan struct{})
				go func() { command, err = f.producer.InterruptWork(ctx, f.cred, request); close(done) }()
				awaitMethod(t, f, "turn/interrupt")
				cancel()
				<-done
			} else {
				command, err = f.producer.InterruptWork(t.Context(), f.cred, request)
			}
			if scenario == "ack" && (err != nil || command.State != "acknowledged") {
				t.Fatal(command, err)
			}
			if scenario == "rejected" {
				var rpc *appserver.RPCError
				if !errors.As(err, &rpc) || command.State != "rejected" {
					t.Fatal(command, err)
				}
			}
			if scenario == "disconnect" || scenario == "invalid" || scenario == "block" {
				if !errors.Is(err, runtime.ErrOutcomeUnknown) || command.State != "delivery_unknown" {
					t.Fatal(command, err)
				}
			}
			if _, err := f.producer.InterruptWork(t.Context(), f.cred, request); err != nil {
				t.Fatal("replayed command", err)
			}
			current, err := f.ledger.ReadWork(t.Context(), f.cred, w.ID)
			if err != nil || current.State != "native_accepted" || journalCount(t, f, "turn/interrupt") != 1 || journalCount(t, f, "turn/start") != 1 || journalCount(t, f, "thread/start") != 1 {
				t.Fatal("interrupt replayed/completed work", current, err)
			}
			if scenario == "ack" || scenario == "rejected" {
				if _, err := f.producer.ReadNative(t.Context(), f.cred, b.ID, true); err != nil {
					t.Fatal("explicit ACK/rejection killed the Peer", err)
				}
			}
		})
	}
}

func TestInterruptTerminalBeforeACKDoesNotRegressOrStartNewWork(t *testing.T) {
	f := setup(t, "work-interrupt-pause")
	b := produce(t, f)
	w := prepareWork(t, f, b, "work", "explicit-interrupt-target")
	w, err := f.producer.ProduceWork(t.Context(), f.cred, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	request := runtime.InterruptWorkRequest{Key: "interrupt", WorkID: w.ID, Generation: w.Generation, ExpectedVersion: w.Version}
	done := make(chan error, 1)
	go func() { _, err := f.producer.InterruptWork(t.Context(), f.cred, request); done <- err }()
	awaitMethod(t, f, "turn/interrupt")
	w = drainUntilEnded(t, f, w.ID)
	if w.Observed.State != "interrupted" {
		t.Fatal(w)
	}
	b, err = f.ledger.ReadBinding(t.Context(), f.cred, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	w2 := prepareWork(t, f, b, "next-explicit-work", "explicit-next-input")
	if err = os.WriteFile(f.proceed, []byte("continue"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	b, err = f.ledger.ReadBinding(t.Context(), f.cred, b.ID)
	if err != nil || b.State != "busy" || b.Outcome != "work_prepared" || b.CleanupStopped || journalCount(t, f, "turn/start") != 1 {
		t.Fatal("ACK regressed terminal or invented stop/start", b, err)
	}
	if _, err = f.producer.ProduceWork(t.Context(), f.cred, w2.ID); err != nil || journalCount(t, f, "turn/start") != 2 {
		t.Fatal("next explicit work failed", err)
	}
}
