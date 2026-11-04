package acpclient

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"time"

	"coordica/internal/jsoninput"
	"coordica/internal/process"
)

// ConfigSelection retains the exact historical response. A match means only
// that the agent reported this value, not effective model/effort or permissions.
type ConfigSelection struct {
	LocalRequestID, AdvertisedVersion uint64
	Evidence                          string
	Confirmed                         bool
	Observed                          ConfigSnapshot
}
type ConfigSelectionError struct {
	Cause   error
	Process process.Result
}

func (e *ConfigSelectionError) Error() string {
	return "ACP configuration call failed: " + e.Cause.Error()
}
func (e *ConfigSelectionError) Unwrap() error { return e.Cause }

type configReply struct {
	result ConfigSelection
	err    error
}
type pendingConfig struct {
	id, advertised  uint64
	configID, value string
	replies         chan configReply
	writeDone       chan struct{}
	received, ready bool
	reply           configReply
}

// SetConfigOption selects one exact advertised ID/value, with a ten-second
// client cap (or shorter caller deadline). Unknown choices are a local subset
// rejection, never a claim that the native harness cannot support them.
func (s Session) SetConfigOption(ctx context.Context, configID, value string) (ConfigSelection, error) {
	if ctx == nil {
		return ConfigSelection{}, ErrProtocol
	}
	if err := ctx.Err(); err != nil {
		return ConfigSelection{}, err
	}
	if _, err := s.capture(); err != nil {
		return ConfigSelection{}, err
	}
	p := s.peer
	p.mu.Lock()
	if p.err != nil {
		p.mu.Unlock()
		return ConfigSelection{}, ErrDisconnected
	}
	if p.prompt != nil || p.configCall != nil {
		p.mu.Unlock()
		return ConfigSelection{}, ErrSessionBusy
	}
	if !configAdvertises(p.configuration, configID, value) {
		p.mu.Unlock()
		return ConfigSelection{}, ErrConfigUnavailable
	}
	if p.lastRequestID >= math.MaxInt64 {
		p.mu.Unlock()
		return ConfigSelection{}, ErrProtocol
	}
	id := p.lastRequestID + 1
	if id < 3 {
		id = 3
	}
	raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": "session/set_config_option", "params": map[string]string{"sessionId": s.id, "configId": configID, "value": value}})
	if len(raw) > maxFrameBytes {
		p.mu.Unlock()
		return ConfigSelection{}, ErrConfigUnavailable
	}
	pending := &pendingConfig{id: id, advertised: p.configuration.Version, configID: configID, value: value, replies: make(chan configReply, 1), writeDone: make(chan struct{})}
	p.configCall, p.lastRequestID = pending, id
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		if p.configCall == pending {
			p.configCall = nil
		}
		p.mu.Unlock()
	}()
	go func() {
		defer close(pending.writeDone)
		if err := p.process.Write(append(raw, '\n')); err != nil {
			p.updates.fault("config_write_failed")
			p.fail(errors.Join(ErrOutcomeUnknown, err))
		}
	}()
	op, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var reply configReply
	select {
	case reply = <-pending.replies:
	case <-p.failed:
		reply = p.configFailure(pending, p.Err())
	case <-op.Done():
		reply = p.configFailure(pending, op.Err())
	}
	if reply.result.Evidence == "native_reported" || isRPCRejection(reply.err) {
		select {
		case <-pending.writeDone:
			if p.Err() == nil {
				return reply.result, reply.err
			}
		case <-p.failed:
		case <-op.Done():
		}
	}
	p.updates.fault("config_connection_failed")
	status, stopErr := p.Close()
	latest := p.configFailure(pending, reply.err)
	if latest.result.Evidence == "native_reported" || isRPCRejection(latest.err) {
		return latest.result, errors.Join(latest.err, &ConfigSelectionError{Cause: ErrDisconnected, Process: status})
	}
	cause := errors.Join(ErrOutcomeUnknown, reply.err)
	if !status.TreeStopped {
		cause = errors.Join(cause, stopErr, process.ErrTreeNotStopped)
	}
	return ConfigSelection{}, &ConfigSelectionError{Cause: cause, Process: status}
}

func configAdvertises(snapshot ConfigSnapshot, id, value string) bool {
	if !snapshot.Available {
		return false
	}
	for _, o := range snapshot.Options {
		if o.ID != id {
			continue
		}
		for _, c := range o.Choices {
			if c.Value == value {
				return true
			}
		}
		for _, g := range o.Groups {
			for _, c := range g.Options {
				if c.Value == value {
					return true
				}
			}
		}
	}
	return false
}
func (p *Peer) configFailure(pending *pendingConfig, cause error) configReply {
	p.mu.Lock()
	defer p.mu.Unlock()
	if pending.ready {
		return pending.reply
	}
	return configReply{err: errors.Join(ErrOutcomeUnknown, cause)}
}

func (p *Peer) receiveConfig(id uint64, result, rpc []byte, hasError bool) error {
	p.mu.Lock()
	pending := p.configCall
	if pending == nil || pending.id != id || pending.received {
		p.mu.Unlock()
		return ErrProtocol
	}
	pending.received = true
	p.mu.Unlock()
	var reply configReply
	if hasError {
		fields, err := object(rpc)
		if err != nil || jsoninput.Required(rpc, "code", "message") != nil {
			return ErrProtocol
		}
		var rejection RPCError
		if decode(fields, "code", &rejection.Code, true) != nil || decode(fields, "message", &rejection.Message, true) != nil {
			return ErrProtocol
		}
		rejection.Data = append(json.RawMessage(nil), fields["data"]...)
		reply.err = &rejection
	} else {
		fields, err := object(result)
		if err != nil {
			return err
		}
		options, err := requiredConfigOptions(fields)
		if err != nil {
			return err
		}
		p.mu.Lock()
		observed := p.replaceConfigLocked(options, true)
		p.mu.Unlock()
		reply.result = ConfigSelection{LocalRequestID: id, AdvertisedVersion: pending.advertised, Evidence: "native_reported", Observed: observed}
		for _, o := range observed.Options {
			if o.ID == pending.configID && o.CurrentValue == pending.value {
				reply.result.Confirmed = true
			}
		}
		if !reply.result.Confirmed {
			reply.err = ErrConfigMismatch
		}
	}
	p.mu.Lock()
	pending.ready, pending.reply = true, reply
	p.mu.Unlock()
	pending.replies <- reply
	return nil
}
