package runtime

import (
	"context"
	"errors"
	"testing"
)

func TestCreateExpiredCallerCannotUnwrapPrivateDiagnostics(t *testing.T) {
	for _, scenario := range []string{"revoke-proof", "recover-proof", "revoke-no-proof", "revoke-private-cleanup"} {
		t.Run(scenario, func(t *testing.T) {
			nativeError := &bindingPrivateFault{"native", "private-native-marker"}
			port := &disclosureBindingPort{actual: newProcessBindingPort(t), transportOnly: scenario != "recover-proof", unknown: scenario == "recover-proof", loseProof: scenario == "revoke-no-proof", createError: nativeError}
			if scenario == "revoke-private-cleanup" {
				port.stopError = &bindingPrivateFault{"cleanup", "private-cleanup-marker"}
			}
			f, p, task, run := bindingFixture(t, port)
			binding := prepareFixture(t, f, p, task, run)
			var recovered Binding
			var recoveryRun Run
			port.afterCreate = func(ctx context.Context, call NativeCreate) error {
				if scenario == "recover-proof" {
					current, _ := readBindingHost(t, f, binding.ID)
					other, err := NewBindingProducer(f.db, p.config, port)
					if err != nil {
						return err
					}
					recovered, err = other.RecoverUnknown(ctx, f.cred, BindingRequest{Key: "recover", BindingID: binding.ID, ExpectedVersion: current.Version, Generation: current.Generation})
					recoveryRun = readDisclosureRun(t, f, run.Run.ID)
					return err
				}
				f.config.Enabled = false
				_, err := f.manager.ConfigureGrant(ctx, f.config, 2)
				return err
			}
			_, err := p.Produce(t.Context(), f.cred, binding.ID)
			if !errors.Is(err, ErrOutcomeUnknown) {
				t.Errorf("expired completion lost unknown: %v", err)
			}
			assertNoBindingPrivateError(t, err)
			if !port.actualStop.TreeStopped || port.actual.creates.Load() != 1 || port.actual.stops.Load() != 1 {
				t.Fatalf("fixture lacked real child Wait proof or repeated I/O: %+v", port.actualStop)
			}
			after, command := readBindingHost(t, f, binding.ID)
			if command.Generation != 1 || command.State != "unknown" {
				t.Error("original claim lost", command)
			}
			if scenario == "recover-proof" {
				if after != recovered || readDisclosureRun(t, f, run.Run.ID) != recoveryRun || command.Observed.ID.Value == "" {
					t.Error("late completion changed recovered binding/run or lost observation", after, command)
				}
			} else if after.State != "unknown" || command.Observed.Outcome != "rejected" {
				t.Error("rejected observation lost", after, command)
			}
			if command.Stop.TreeStopped == port.loseProof {
				t.Error("returned/lost proof not preserved", command.Stop)
			}
			if _, again := p.Produce(t.Context(), f.cred, binding.ID); again == nil || port.actual.creates.Load() != 1 || port.actual.stops.Load() != 1 {
				t.Error("expired/replayed call repeated native I/O", again)
			}
			assertDisclosureNotInNotifications(t, f)
			reopenFixture(t, &f)
			persisted, stored := readBindingHost(t, f, binding.ID)
			if persisted != after || stored != command {
				t.Error("reopen changed original facts", persisted, stored)
			}
		})
	}
}

func TestCreateCurrentCallerRetainsRejectionAndOwnUnknownDiagnostics(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(map[bool]string{false: "explicit-rejection", true: "own-unknown"}[unknown], func(t *testing.T) {
			nativeError := &bindingPrivateFault{"native", "private-native-marker"}
			port := &disclosureBindingPort{actual: newProcessBindingPort(t), transportOnly: !unknown, unknown: unknown, createError: nativeError}
			f, p, task, run := bindingFixture(t, port)
			binding := prepareFixture(t, f, p, task, run)
			got, err := p.Produce(t.Context(), f.cred, binding.ID)
			var private *bindingPrivateFault
			if !errors.As(err, &private) || private != nativeError || errors.Is(err, ErrOutcomeUnknown) != unknown {
				t.Error("current native classification lost", got, err)
			}
			if !unknown && got.State != "unbound" {
				t.Error("known rejection became uncertain", got)
			}
			stored, command := readBindingHost(t, f, binding.ID)
			if !port.actualStop.TreeStopped || !stored.CleanupStopped || !command.Stop.TreeStopped || port.actual.creates.Load() != 1 || port.actual.stops.Load() != 1 {
				t.Error("actual stop not retained", stored, command)
			}
			assertDisclosureNotInNotifications(t, f)
		})
	}
}
