package runtime

import (
	"errors"
	"testing"
	"time"
)

func TestReleaseExpiredCallerCannotUnwrapPrivateStopDiagnostics(t *testing.T) {
	for _, change := range []string{"revoke", "scope", "recover"} {
		for _, proof := range []bool{true, false} {
			name := change + map[bool]string{true: "-proof", false: "-no-proof"}[proof]
			t.Run(name, func(t *testing.T) {
				port := &disclosureBindingPort{actual: newProcessBindingPort(t), loseProof: !proof, stopError: &bindingPrivateFault{"cleanup", "private-cleanup-marker"}, stopEntered: make(chan NativeCreate, 1), stopContinue: make(chan struct{})}
				f, p, task, run := bindingFixture(t, port)
				binding := prepareFixture(t, f, p, task, run)
				ready, err := p.Produce(t.Context(), f.cred, binding.ID)
				if err != nil {
					t.Fatal(err)
				}
				request := BindingRequest{Key: "release", BindingID: ready.ID, ExpectedVersion: ready.Version, Generation: ready.Generation}
				done := make(chan error, 1)
				go func() { _, err := p.Release(t.Context(), f.cred, request); done <- err }()
				var call NativeCreate
				select {
				case call = <-port.stopEntered:
				case <-time.After(3 * time.Second):
					t.Fatal("Stop did not reach bounded barrier")
				}
				var recovered Binding
				var recoveryRun Run
				if change == "recover" {
					current, _ := readBindingHost(t, f, binding.ID)
					other, err := NewBindingProducer(f.db, p.config, port)
					if err != nil {
						t.Fatal(err)
					}
					recovered, err = other.RecoverUnknown(t.Context(), f.cred, BindingRequest{Key: "recover", BindingID: binding.ID, ExpectedVersion: current.Version, Generation: current.Generation})
					if err != nil {
						t.Fatal(err)
					}
					recoveryRun = readDisclosureRun(t, f, run.Run.ID)
				} else {
					if change == "scope" {
						f.config.Targets = nil
					} else {
						f.config.Enabled = false
					}
					if _, err := f.manager.ConfigureGrant(t.Context(), f.config, 2); err != nil {
						t.Fatal(err)
					}
				}
				close(port.stopContinue)
				err = <-done
				if !errors.Is(err, ErrOutcomeUnknown) || !errors.Is(err, ErrStopUnconfirmed) {
					t.Errorf("stop uncertainty misclassified: %v", err)
				}
				assertNoBindingPrivateError(t, err)
				if !port.actualStop.TreeStopped || port.actual.creates.Load() != 1 || port.actual.stops.Load() != 1 {
					t.Fatalf("Stop was not one actual child Wait: %+v", port.actualStop)
				}
				after, command := readBindingHost(t, f, binding.ID)
				if command.ID != call.CommandID || command.Operation != "release" || command.Generation != 1 || command.State != "unknown" || command.Observed.Outcome != "stop_unconfirmed" || command.Stop.TreeStopped != proof {
					t.Error("original Stop observation lost", command)
				}
				if change == "recover" {
					if after != recovered || readDisclosureRun(t, f, run.Run.ID) != recoveryRun {
						t.Error("late Stop changed new generation", after, recovered)
					}
				} else if after.State != "unknown" || after.CleanupStopped != proof {
					t.Error("cleanup invented completion", after)
				}
				if _, err := p.Release(t.Context(), f.cred, request); err == nil || port.actual.stops.Load() != 1 || port.actual.creates.Load() != 1 {
					t.Error("uncertain release repeated I/O", err)
				}
				assertDisclosureNotInNotifications(t, f)
				reopenFixture(t, &f)
				stored, storedCommand := readBindingHost(t, f, binding.ID)
				if stored != after || storedCommand != command {
					t.Error("Stop facts lost on reopen", stored, storedCommand)
				}
			})
		}
	}
}

func TestReleaseCurrentCleanupAuthorityRetainsDiagnosticsAndRetiredCleanup(t *testing.T) {
	for _, scenario := range []string{"current-error", "retired-error", "retired-success"} {
		t.Run(scenario, func(t *testing.T) {
			stopError := &bindingPrivateFault{"cleanup", "private-cleanup-marker"}
			port := &disclosureBindingPort{actual: newProcessBindingPort(t)}
			f, p, task, run := bindingFixture(t, port)
			binding := prepareFixture(t, f, p, task, run)
			ready, err := p.Produce(t.Context(), f.cred, binding.ID)
			if err != nil {
				t.Fatal(err)
			}
			if scenario != "current-error" {
				// Current cleanup scope is retained. Version changes and a disabled
				// instance must not be treated as revocation of the owned old resource.
				if _, err := f.manager.ConfigureGrant(t.Context(), f.config, 2); err != nil {
					t.Fatal(err)
				}
				if _, err := f.manager.ConfigureInstance(t.Context(), Instance{ID: f.target.InstanceID, ApplicationID: f.config.ApplicationID, NodeID: f.target.NodeID, Harness: f.target.Harness, ConfigRef: "retired-config-v2", Support: "unknown", Enabled: false}, 1); err != nil {
					t.Fatal(err)
				}
			}
			if scenario != "retired-success" {
				port.stopError = stopError
			}
			request := BindingRequest{Key: "release", BindingID: ready.ID, ExpectedVersion: ready.Version, Generation: ready.Generation}
			result, err := p.Release(t.Context(), f.cred, request)
			if scenario == "retired-success" {
				if err != nil || result.State != "released" {
					t.Error("legitimate retired cleanup rejected", result, err)
				}
			} else {
				var private *bindingPrivateFault
				if !errors.As(err, &private) || private != stopError || !errors.Is(err, ErrOutcomeUnknown) || !errors.Is(err, ErrStopUnconfirmed) {
					t.Error("current diagnostic or classification lost", err)
				}
			}
			after, command := readBindingHost(t, f, binding.ID)
			if !port.actualStop.TreeStopped || !command.Stop.TreeStopped || !after.CleanupStopped || port.actual.creates.Load() != 1 || port.actual.stops.Load() != 1 {
				t.Error("actual Stop evidence missing", after, command)
			}
			assertDisclosureNotInNotifications(t, f)
		})
	}
}
