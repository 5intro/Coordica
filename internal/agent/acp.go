package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// One shared ACP driver owns the session state machine. Optional resume,
// close, authentication and review modes are negotiated, not brand guesses.
func executeACP(ctx context.Context, def Definition, req Request, onEvent func(Event)) (Result, error) {
	peer, err := startRPC(def, req, onEvent)
	if err != nil {
		return Result{ExitCode: -1}, err
	}
	defer peer.stopWriter()
	s := peer.process
	nextID := 0
	expectedID := 0
	pendingMethod := ""
	send := func(method string, params any) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		nextID++
		expectedID = nextID
		pendingMethod = method
		return peer.send(map[string]any{"jsonrpc": "2.0", "id": nextID, "method": method, "params": params})
	}
	notify := func(method string, params any) error {
		return peer.send(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	}
	err = send("initialize", map[string]any{"protocolVersion": 1, "clientInfo": map[string]string{"name": "coordica", "version": "0.1.1"}, "clientCapabilities": map[string]any{"fs": map[string]bool{"readTextFile": false, "writeTextFile": false}, "terminal": false}})
	if err != nil {
		s.Stop()
	}
	result := Result{ExitCode: -1}
	protocolErr := err
	complete, cancelled, teardown := false, false, false
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
	canLoad, canResume, canClose := false, false, false
	calls := map[string]acpMetadata{}
	promptStarted := false
	cancelRun := func() {
		if cancelled {
			return
		}
		cancelInput = nil
		cancelled = true
		if result.SessionID != "" && def.Cancel {
			if err := notify("session/cancel", map[string]string{"sessionId": result.SessionID}); err != nil {
				protocolErr = errors.Join(protocolErr, err)
			}
		}
		armStop(750 * time.Millisecond)
	}
	startSession := func() error {
		params := map[string]any{"cwd": req.WorkDir, "mcpServers": []any{}}
		method := "session/new"
		if req.ResumeSession != "" {
			if (def.ResumeMethod == "session/load" && !canLoad) || (def.ResumeMethod == "session/resume" && !canResume) {
				return errors.New("ACP peer does not advertise registered resume capability")
			}
			method = def.ResumeMethod
			params["sessionId"] = req.ResumeSession
			result.SessionID = req.ResumeSession
		}
		return send(method, params)
	}
	prompt := func() error {
		// session/load may replay prior messages before its response. Only the
		// current prompt belongs in this execution's output and review result.
		result.OutputText = ""
		calls = map[string]acpMetadata{}
		promptStarted = true
		return send("session/prompt", map[string]any{"sessionId": result.SessionID, "prompt": []map[string]string{{"type": "text", "text": req.Prompt}}})
	}
	shutdown := func() error {
		armStop(2 * time.Second)
		if canClose && def.CloseMethod != "" {
			return send(def.CloseMethod, map[string]string{"sessionId": result.SessionID})
		}
		return peer.closeInput()
	}
	for {
		select {
		case err := <-peer.writeErrors:
			protocolErr = errors.Join(protocolErr, err)
			s.Stop()
		case <-cancelInput:
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
				JSONRPC string          `json:"jsonrpc"`
				ID      json.RawMessage `json:"id"`
				Method  string          `json:"method"`
				Params  json.RawMessage `json:"params"`
				Result  json.RawMessage `json:"result"`
				Error   json.RawMessage `json:"error"`
			}
			if json.Unmarshal(output.Line, &message) != nil || message.JSONRPC != "2.0" {
				protocolErr = ErrProtocol
				s.Stop()
				continue
			}
			emit(onEvent, "acp.message", json.RawMessage(output.Line))
			if ctx.Err() != nil {
				cancelRun()
			}
			if cancelled {
				if message.Method == "session/request_permission" && len(message.ID) > 0 {
					if err := peer.send(map[string]any{"jsonrpc": "2.0", "id": message.ID, "result": map[string]any{"outcome": map[string]string{"outcome": "cancelled"}}}); err != nil {
						protocolErr = errors.Join(protocolErr, err)
					}
				}
				continue
			}
			if message.Method != "" {
				if len(message.ID) != 0 {
					reply := map[string]any{"jsonrpc": "2.0", "id": message.ID}
					if message.Method == "session/request_permission" {
						option, allowed := acpPermissionFromCalls(req, result.SessionID, message.Params, calls)
						if ctx.Err() != nil {
							allowed = false
							cancelRun()
						}
						if allowed {
							reply["result"] = map[string]any{"outcome": map[string]string{"outcome": "selected", "optionId": option}}
						} else {
							reply["result"] = map[string]any{"outcome": map[string]string{"outcome": "cancelled"}}
							emit(onEvent, "permission_denied", map[string]string{"reason": "unverifiable or unauthorized ACP operation"})
						}
					} else {
						reply["error"] = map[string]any{"code": -32601, "message": "unsupported client method; owner input may be required"}
						emit(onEvent, "input_required", map[string]string{"method": message.Method})
					}
					if err := peer.send(reply); err != nil {
						protocolErr = errors.Join(protocolErr, err)
						s.Stop()
					}
				} else if message.Method == "session/update" {
					var params struct {
						SessionID string `json:"sessionId"`
						Update    struct {
							Kind    string          `json:"sessionUpdate"`
							Content json.RawMessage `json:"content"`
						} `json:"update"`
					}
					if json.Unmarshal(message.Params, &params) != nil || params.SessionID != result.SessionID {
						protocolErr = ErrProtocol
						s.Stop()
						continue
					}
					if promptStarted && (params.Update.Kind == "tool_call" || params.Update.Kind == "tool_call_update") {
						var container struct {
							Update json.RawMessage `json:"update"`
						}
						json.Unmarshal(message.Params, &container)
						if err := recordACPCall(calls, container.Update); err != nil {
							protocolErr = err
							s.Stop()
							continue
						}
					}
					if params.Update.Kind == "agent_message_chunk" {
						var content struct{ Type, Text string }
						if json.Unmarshal(params.Update.Content, &content) != nil {
							protocolErr = ErrProtocol
							s.Stop()
							continue
						}
						if content.Type == "text" {
							result.OutputText += content.Text
						}
					}
					if len(result.OutputText) > 8<<20 {
						protocolErr = errors.New("agent result exceeds size limit")
						s.Stop()
					}
				}
				continue
			}
			var id int
			if json.Unmarshal(message.ID, &id) != nil || id != expectedID {
				protocolErr = fmt.Errorf("%w: unexpected response ID", ErrProtocol)
				s.Stop()
				continue
			}
			if len(message.Error) > 0 && string(message.Error) != "null" {
				protocolErr = rpcError(message.Error)
				s.Stop()
				continue
			}
			if len(message.Result) == 0 || string(message.Result) == "null" {
				protocolErr = ErrProtocol
				s.Stop()
				continue
			}
			var nextErr error
			switch pendingMethod {
			case "initialize":
				var init struct {
					ProtocolVersion   int `json:"protocolVersion"`
					AgentCapabilities struct {
						LoadSession bool                       `json:"loadSession"`
						Sessions    map[string]json.RawMessage `json:"sessionCapabilities"`
					} `json:"agentCapabilities"`
					AuthMethods []struct {
						ID string `json:"id"`
					} `json:"authMethods"`
				}
				if json.Unmarshal(message.Result, &init) != nil || init.ProtocolVersion != 1 {
					protocolErr = errors.New("unsupported ACP protocol version")
					s.Stop()
					continue
				}
				canLoad = init.AgentCapabilities.LoadSession
				_, canResume = init.AgentCapabilities.Sessions["resume"]
				_, canClose = init.AgentCapabilities.Sessions["close"]
				if def.AuthMethod != "" {
					advertised := false
					for _, method := range init.AuthMethods {
						advertised = advertised || method.ID == def.AuthMethod
					}
					if !advertised {
						nextErr = errors.New("ACP peer does not advertise registered authentication method")
					} else {
						nextErr = send("authenticate", map[string]string{"methodId": def.AuthMethod})
					}
				} else {
					nextErr = startSession()
				}
			case "authenticate":
				nextErr = startSession()
			case "session/new", "session/load", "session/resume":
				var created struct {
					SessionID string `json:"sessionId"`
					Modes     *struct {
						AvailableModes []struct {
							ID string `json:"id"`
						} `json:"availableModes"`
					} `json:"modes"`
				}
				if json.Unmarshal(message.Result, &created) != nil {
					nextErr = ErrProtocol
					break
				}
				if req.ResumeSession == "" {
					if created.SessionID == "" {
						nextErr = ErrProtocol
						break
					}
					result.SessionID = created.SessionID
				}
				if isReview(req) && def.ReviewMode != "" {
					available := false
					if created.Modes != nil {
						for _, mode := range created.Modes.AvailableModes {
							available = available || mode.ID == def.ReviewMode
						}
					}
					if !available {
						nextErr = errors.New("ACP peer does not advertise required read-only review mode")
					} else {
						nextErr = send("session/set_mode", map[string]string{"sessionId": result.SessionID, "modeId": def.ReviewMode})
					}
				} else {
					nextErr = prompt()
				}
			case "session/set_mode":
				nextErr = prompt()
			case "session/prompt":
				var final struct {
					StopReason string `json:"stopReason"`
				}
				if json.Unmarshal(message.Result, &final) != nil || final.StopReason != "end_turn" {
					protocolErr = errors.New("ACP prompt did not complete normally")
				} else {
					complete = true
				}
				nextErr = shutdown()
			case "session/close":
				armStop(2 * time.Second)
				nextErr = peer.closeInput()
			default:
				nextErr = ErrProtocol
			}
			if nextErr != nil {
				protocolErr = errors.Join(protocolErr, nextErr)
				s.Stop()
			}
		}
	}
}
