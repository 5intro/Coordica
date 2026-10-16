package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// pi RPC 1.1.0+ distinguishes low-level agent_end from settled completion.
// An accepted prompt/abort command is not a task terminal. SessionID is an
// opaque session-file handle. Resume requires the same canonical workdir:
// pi restores header.cwd and cannot rebind a session to a new worktree.
func executePi(ctx context.Context, def Definition, req Request, onEvent func(Event)) (Result, error) {
	peer, err := startRPC(def, req, onEvent)
	if err != nil {
		return Result{ExitCode: -1}, err
	}
	defer peer.stopWriter()
	s := peer.process
	nextID := 0
	pending := map[string]string{}
	send := func(command string, fields map[string]any) error {
		if err := ctx.Err(); err != nil && command != "clear_queue" && command != "abort" {
			return err
		}
		nextID++
		id := fmt.Sprintf("coordica-%d", nextID)
		pending[id] = command
		if fields == nil {
			fields = map[string]any{}
		}
		fields["id"] = id
		fields["type"] = command
		return peer.send(fields)
	}
	err = send("get_state", nil)
	if err != nil {
		s.Stop()
	}
	result := Result{ExitCode: -1}
	protocolErr := err
	promptSent, promptAccepted, settled, finalStateRequested := false, false, false, false
	modelConfigured := false
	complete, cancelled, teardown := false, false, false
	lastStop := ""
	cancelInput := ctx.Done()
	var stopTimer <-chan time.Time
	var timer *time.Timer
	armStop := func(duration time.Duration) {
		if timer != nil {
			timer.Stop()
		}
		timer = time.NewTimer(duration)
		stopTimer = timer.C
	}
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	cancelRun := func() {
		if cancelled {
			return
		}
		cancelInput = nil
		cancelled = true
		protocolErr = errors.Join(protocolErr, send("clear_queue", nil), send("abort", nil))
		armStop(750 * time.Millisecond)
	}
	finalState := func() error {
		if settled && promptAccepted && !finalStateRequested && !cancelled {
			finalStateRequested = true
			armStop(2 * time.Second)
			return send("get_state", nil)
		}
		return nil
	}
	for {
		select {
		case err := <-peer.writeErrors:
			protocolErr = errors.Join(protocolErr, err)
			s.Stop()
		case <-cancelInput:
			// Clearing queued work precedes abort; the ack alone never frees a lease.
			cancelRun()
		case <-stopTimer:
			stopTimer = nil
			teardown = complete && !cancelled && protocolErr == nil
			s.Stop()
		case output, ok := <-s.Output():
			if !ok {
				status, waitErr := s.Wait()
				result.ExitCode, result.TreeStopped = status.ExitCode, status.TreeStopped
				if cancelled {
					return result, errors.Join(ctx.Err(), waitErr, protocolErr)
				}
				if !complete || result.SessionID == "" {
					protocolErr = errors.Join(protocolErr, ErrProtocol)
				}
				return result, errors.Join(rpcFinished(status, waitErr, teardown), protocolErr)
			}
			if output.Stream != "stdout" {
				continue
			}
			var message struct {
				Type    string          `json:"type"`
				ID      string          `json:"id"`
				Command string          `json:"command"`
				Success *bool           `json:"success"`
				Error   string          `json:"error"`
				Data    json.RawMessage `json:"data"`
				Aborted *bool           `json:"aborted"`
				Method  string          `json:"method"`
				Message json.RawMessage `json:"message"`
			}
			if json.Unmarshal(output.Line, &message) != nil || message.Type == "" {
				protocolErr = ErrProtocol
				s.Stop()
				continue
			}
			onEvent(Event{Type: message.Type, Data: sanitize(output.Line)})
			if ctx.Err() != nil {
				cancelRun()
			}
			if cancelled {
				continue
			}
			var nextErr error
			switch message.Type {
			case "response":
				command, known := pending[message.ID]
				if !known || message.Command != command || message.Success == nil {
					nextErr = ErrProtocol
					break
				}
				delete(pending, message.ID)
				if !*message.Success {
					nextErr = errors.New("pi RPC command failed: " + redactText(message.Error, knownSecrets()))
					break
				}
				switch command {
				case "get_state":
					var state struct {
						Model         *struct{ ID, Provider string } `json:"model"`
						ThinkingLevel string                         `json:"thinkingLevel"`
						SessionID     string                         `json:"sessionId"`
						SessionFile   string                         `json:"sessionFile"`
						Streaming     *bool                          `json:"isStreaming"`
						Compacting    *bool                          `json:"isCompacting"`
						Pending       *int                           `json:"pendingMessageCount"`
					}
					if json.Unmarshal(message.Data, &state) != nil || state.SessionID == "" || state.SessionFile == "" || state.Streaming == nil || state.Compacting == nil || state.Pending == nil {
						nextErr = ErrProtocol
						break
					}
					if result.SessionID != "" && result.SessionID != state.SessionFile {
						nextErr = errors.New("pi session changed during execution")
						break
					}
					result.SessionID = state.SessionFile
					if !promptSent {
						if req.ResumeSession != "" && !sameSessionFile(req.ResumeSession, state.SessionFile) {
							nextErr = errors.New("pi did not load the exact requested session")
							break
						}
						if *state.Streaming || *state.Compacting || *state.Pending != 0 {
							nextErr = errors.New("pi session is not idle before configuration")
							break
						}
						if req.Model != "" {
							if !modelConfigured {
								nextErr = send("get_available_models", nil)
								break
							}
							provider, id, _ := splitPiModel(req.Model)
							if state.Model == nil || state.Model.ID != id || state.Model.Provider != provider || req.ReasoningEffort != "" && state.ThinkingLevel != req.ReasoningEffort {
								nextErr = errors.New("pi did not apply the exact requested model options")
								break
							}
						}
						promptSent = true
						nextErr = send("prompt", map[string]any{"message": req.Prompt})
					} else if finalStateRequested {
						if *state.Streaming || *state.Compacting || *state.Pending != 0 || lastStop != "stop" {
							nextErr = errors.New("pi settled without a successful idle assistant result")
							break
						}
						complete = true
						nextErr = peer.closeInput()
						armStop(2 * time.Second)
					} else {
						nextErr = ErrProtocol
					}
				case "get_available_models":
					var catalog struct {
						Models []struct{ ID, Provider string }
					}
					if json.Unmarshal(message.Data, &catalog) != nil || len(catalog.Models) > 64 {
						nextErr = ErrProtocol
						break
					}
					provider, id, err := splitPiModel(req.Model)
					if err != nil {
						nextErr = err
						break
					}
					found := false
					for _, model := range catalog.Models {
						found = found || model.ID == id && model.Provider == provider
					}
					if !found {
						nextErr = errors.New("pi model is no longer available")
						break
					}
					nextErr = send("set_model", map[string]any{"provider": provider, "modelId": id})
				case "set_model":
					var selected struct{ ID, Provider string }
					provider, id, _ := splitPiModel(req.Model)
					if json.Unmarshal(message.Data, &selected) != nil || selected.ID != id || selected.Provider != provider {
						nextErr = ErrProtocol
						break
					}
					modelConfigured = true
					if req.ReasoningEffort != "" {
						nextErr = send("get_available_thinking_levels", nil)
					} else {
						nextErr = send("get_state", nil)
					}
				case "get_available_thinking_levels":
					var levels struct{ Levels []string }
					if json.Unmarshal(message.Data, &levels) != nil || len(levels.Levels) > 16 {
						nextErr = ErrProtocol
						break
					}
					found := false
					for _, level := range levels.Levels {
						found = found || level == req.ReasoningEffort
					}
					if !found {
						nextErr = errors.New("pi effort is not supported by this model")
						break
					}
					nextErr = send("set_thinking_level", map[string]any{"level": req.ReasoningEffort})
				case "set_thinking_level":
					nextErr = send("get_state", nil)
				case "prompt":
					promptAccepted = true
					nextErr = finalState()
				case "clear_queue", "abort":
					if !cancelled {
						nextErr = ErrProtocol
					}
				default:
					nextErr = ErrProtocol
				}
			case "message_end":
				if !promptSent {
					nextErr = ErrProtocol
					break
				}
				var ended struct {
					Role       string          `json:"role"`
					StopReason string          `json:"stopReason"`
					Content    json.RawMessage `json:"content"`
				}
				if json.Unmarshal(message.Message, &ended) != nil {
					nextErr = ErrProtocol
					break
				}
				if ended.Role == "assistant" {
					lastStop = ended.StopReason
					var contents []struct{ Type, Text string }
					if json.Unmarshal(ended.Content, &contents) != nil {
						nextErr = ErrProtocol
						break
					}
					var text strings.Builder
					for _, content := range contents {
						if content.Type == "text" {
							text.WriteString(content.Text)
						}
					}
					result.OutputText = text.String()
					if len(result.OutputText) > 8<<20 {
						nextErr = errors.New("agent result exceeds size limit")
					}
				}
			case "agent_settled":
				if !promptSent || message.Aborted == nil {
					nextErr = ErrProtocol
					break
				}
				if *message.Aborted && !cancelled {
					nextErr = errors.New("pi agent settled after abort")
					break
				}
				settled = true
				nextErr = finalState()
			case "extension_ui_request":
				// Never manufacture a human answer. The response cancels a blocking UI
				// request so this headless transport cannot hang indefinitely.
				if message.Method != "select" && message.Method != "confirm" && message.Method != "input" && message.Method != "editor" {
					break
				}
				if message.ID == "" {
					nextErr = ErrProtocol
					break
				}
				nextErr = peer.send(map[string]any{"type": "extension_ui_response", "id": message.ID, "cancelled": true})
				emit(onEvent, "input_required", map[string]string{"reason": "pi extension requested owner input"})
				// agent_end, retry and compaction events are diagnostic, not terminals.
			}
			if nextErr != nil {
				protocolErr = errors.Join(protocolErr, nextErr)
				s.Stop()
			}
		}
	}
}

func sameSessionFile(requested, actual string) bool {
	if !filepath.IsAbs(requested) {
		return requested == actual
	}
	requested, actual = filepath.Clean(requested), filepath.Clean(actual)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(requested, actual)
	}
	return requested == actual
}
