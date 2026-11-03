package runtime

import (
	"context"
	"math"

	"coordica/internal/store"
)

// BindingProducer is trusted host wiring, like Manager. A new producer has a
// fresh owner identity and performs no recovery, claim, takeover or native I/O.
type BindingProducer struct {
	ledger *Ledger
	config BindingConfig
	port   BindingPort
	owner  string
}

func NewBindingProducer(db *store.Store, config BindingConfig, port BindingPort) (*BindingProducer, error) {
	if db == nil || port == nil || !validBindingConfig(config) {
		return nil, ErrInvalid
	}
	owner, err := freshID("owner")
	if err != nil {
		return nil, err
	}
	return &BindingProducer{ledger: New(db), config: config, port: port, owner: owner}, nil
}

func validBindingConfig(config BindingConfig) bool {
	return validTarget(config.Target) && identifier(config.InstanceConfigRef, 512) && identifier(config.ProfileRef, 512) &&
		identifier(config.WorkspaceFingerprint, 128) && identifier(config.ProtocolVersion, 128) &&
		identifier(config.ApprovalPolicy, 128) && identifier(config.Sandbox, 128)
}

func bindingFor(tx *store.Tx, grant Grant, id string, prepare bool) (Binding, Task, Run, error) {
	var binding Binding
	if err := tx.Get(kindBinding, id, &binding); err != nil {
		return binding, Task{}, Run{}, err
	}
	if binding.ApplicationID != grant.ApplicationID {
		return Binding{}, Task{}, Run{}, ErrForbidden
	}
	task, err := taskFor(tx, grant, binding.TaskID)
	var run Run
	if err == nil {
		run, err = runFor(tx, grant, binding.TaskID, binding.RunID, prepare)
	}
	if err == nil && (run.Generation != binding.RunGeneration || run.ContextID != binding.ContextID ||
		run.ContextVersion != binding.ContextVersion || run.Target != binding.Config.Target || run.InstanceConfigRef != binding.Config.InstanceConfigRef ||
		run.AuthorizationVersion != binding.AuthorizationVersion || run.PrincipalID != binding.PrincipalID) {
		err = ErrConflict
	}
	return binding, task, run, err
}

func liveBinding(grant Grant, task Task, run Run) error {
	if task.State != "open" || task.CurrentRunID != run.ID || run.State != "registered" {
		return ErrConflict
	}
	if grant.ID != run.PrincipalID || grant.Version != run.AuthorizationVersion {
		return ErrForbidden
	}
	return nil
}

func (p *BindingProducer) PrepareBinding(ctx context.Context, credential Credential, request PrepareBindingRequest) (Binding, error) {
	if p == nil || !identifier(request.TaskID, 128) || !identifier(request.RunID, 128) || request.RunGeneration <= 0 || request.ExpectedVersion <= 0 {
		return Binding{}, ErrInvalid
	}
	id, err := freshID("session")
	if err != nil {
		return Binding{}, err
	}
	commandID, err := freshID("command")
	if err != nil {
		return Binding{}, err
	}
	var task Task
	var run Run
	payload := struct {
		Request PrepareBindingRequest `json:"request"`
		Config  BindingConfig         `json:"host_config"`
	}{request, p.config}
	return mutate(p.ledger, ctx, credential, SessionControl, request.Key, "binding.prepared", payload,
		func(tx *store.Tx, grant Grant) (err error) {
			if task, err = taskFor(tx, grant, request.TaskID); err == nil {
				run, err = runFor(tx, grant, task.ID, request.RunID, true)
			}
			if err == nil && (run.Generation != request.RunGeneration || run.Target != p.config.Target || run.InstanceConfigRef != p.config.InstanceConfigRef) {
				err = ErrConflict
			}
			if err == nil && (grant.ID != run.PrincipalID || grant.Version != run.AuthorizationVersion) {
				err = ErrForbidden
			}
			return err
		}, func(tx *store.Tx, grant Grant) (Binding, string, bool, error) {
			if err := writable(task, request.ExpectedVersion); err != nil {
				return Binding{}, "", false, err
			}
			if err := liveBinding(grant, task, run); err != nil {
				return Binding{}, "", false, err
			}
			if run.NativeDisposition != "not_started" || run.Version == math.MaxInt64 {
				return Binding{}, "", false, ErrStopUnconfirmed
			}
			binding := Binding{ID: id, ApplicationID: grant.ApplicationID, TaskID: task.ID, RunID: run.ID,
				RunGeneration: run.Generation, ContextID: run.ContextID, ContextVersion: run.ContextVersion,
				AuthorizationVersion: grant.Version, PrincipalID: grant.ID, Config: p.config, Version: 1,
				Generation: 1, CommandID: commandID, State: "creating", Outcome: "pending"}
			if err := putNew(tx, kindBindingRun, run.ID, binding.ID); err != nil {
				return Binding{}, "", false, err
			}
			if err := putNew(tx, kindBinding, binding.ID, binding); err != nil {
				return Binding{}, "", false, err
			}
			command := BindingCommand{ID: commandID, BindingID: binding.ID, Generation: 1, Operation: "create", State: "pending"}
			if err := putNew(tx, kindBindingCommand, command.ID, command); err != nil {
				return Binding{}, "", false, err
			}
			run.NativeDisposition, run.Version = "session_pending", run.Version+1
			task.Version++
			if err := tx.Put(kindRun, run.ID, run); err != nil {
				return Binding{}, "", false, err
			}
			return binding, binding.ID, true, tx.Put(kindTask, task.ID, task)
		})
}

// ReadBinding reads scoped ledger facts, not native history or resource health.
func (l *Ledger) ReadBinding(ctx context.Context, credential Credential, id string) (Binding, error) {
	var binding Binding
	if l == nil || l.db == nil || !identifier(id, 128) {
		return binding, ErrInvalid
	}
	err := l.db.View(ctx, func(tx *store.Tx) error {
		grant, err := authorize(tx, credential, Read)
		if err == nil {
			binding, _, _, err = bindingFor(tx, grant, id, false)
		}
		return err
	})
	if err != nil {
		return Binding{}, err
	}
	return binding, nil
}
