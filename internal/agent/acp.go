package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"coordica/internal/process"
)

// This adapter owns a single ACP session. All replies are matched by ID and
// unsupported server requests are rejected; terminal presentation is unused.
func executeACP(ctx context.Context, req Request, onEvent func(Event)) (Result, error) {
	procCtx, stopProcess := context.WithCancel(context.Background())
	defer stopProcess()
	s, err := process.Start(procCtx, process.Spec{Executable: req.Executable, Args: []string{"--profile", "acp"}, Dir: req.WorkDir, Env: developmentEnv()})
	if err != nil {
		return Result{ExitCode: -1}, err
	}
	emit(onEvent, "process.started", map[string]int{"pid": s.PID()})
	// A protocol peer may stop reading stdin while producing output. Keep
	// writes off the receive/cancellation loop so a full pipe cannot suppress
	// the cancellation timer or lease revocation.
	writes := make(chan []byte, 8)
	writeErrors := make(chan error, 1)
	go func() {
		for {
			select {
			case <-procCtx.Done():
				return
			case data := <-writes:
				if err := s.Write(data); err != nil {
					writeErrors <- err
					return
				}
			}
		}
	}()
	writeRaw := func(data []byte) error {
		select {
		case writes <- append(data, '\n'):
			return nil
		default:
			return errors.New("ACP write queue exhausted")
		}
	}
	write := func(id any, method string, params any) error {
		message := map[string]any{"jsonrpc": "2.0", "method": method, "params": params}
		if id != nil {
			message["id"] = id
		}
		data, err := json.Marshal(message)
		if err != nil {
			return err
		}
		return writeRaw(data)
	}
	if err = write(1, "initialize", map[string]any{"protocolVersion": 1, "clientInfo": map[string]string{"name": "coordica", "version": "0.1.0"}, "clientCapabilities": map[string]any{}}); err != nil {
		s.Stop()
	}
	result := Result{ExitCode: -1}
	var protocolErr error = err
	expectedID := 1
	complete := false
	cancelled := false
	cancelInput := ctx.Done()
	var stopTimer <-chan time.Time
	var timer *time.Timer
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	for {
		select {
		case err := <-writeErrors:
			protocolErr = err
			s.Stop()
		case <-cancelInput:
			cancelInput = nil
			cancelled = true
			if result.SessionID != "" {
				write(nil, "session/cancel", map[string]string{"sessionId": result.SessionID})
			}
			timer = time.NewTimer(750 * time.Millisecond)
			stopTimer = timer.C
		case <-stopTimer:
			stopTimer = nil
			s.Stop()
		case output, ok := <-s.Output():
			if !ok {
				status, waitErr := s.Wait()
				result.ExitCode, result.TreeStopped = status.ExitCode, status.TreeStopped
				if cancelled {
					return result, errors.Join(ctx.Err(), waitErr, protocolErr)
				}
				if !complete {
					protocolErr = errors.Join(protocolErr, ErrProtocol)
				}
				return result, errors.Join(waitErr, protocolErr)
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
			if message.Method != "" {
				if len(message.ID) != 0 {
					// The current DSH bridge only identifies the call. No local
					// grant can be inferred from that opaque ID or a display title.
					reply := map[string]any{"jsonrpc": "2.0", "id": message.ID}
					if message.Method == "session/request_permission" {
						reply["result"] = map[string]any{"outcome": map[string]string{"outcome": "cancelled"}}
						emit(onEvent, "permission_denied", map[string]string{"reason": "unverifiable ACP permission request"})
					} else {
						reply["error"] = map[string]any{"code": -32601, "message": "unsupported client method"}
					}
					data, _ := json.Marshal(reply)
					if err := writeRaw(data); err != nil {
						protocolErr = err
						s.Stop()
					}
				} else if message.Method == "session/update" {
					var params struct {
						SessionID string `json:"sessionId"`
						Update    struct {
							Kind    string                      `json:"sessionUpdate"`
							Content struct{ Type, Text string } `json:"content"`
						} `json:"update"`
					}
					if json.Unmarshal(message.Params, &params) != nil || params.SessionID != result.SessionID {
						protocolErr = ErrProtocol
						s.Stop()
						continue
					}
					if params.Update.Kind == "agent_message_chunk" && params.Update.Content.Type == "text" {
						result.OutputText += params.Update.Content.Text
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
			switch id {
			case 1:
				var init struct {
					ProtocolVersion   int `json:"protocolVersion"`
					AgentCapabilities struct {
						Sessions map[string]json.RawMessage `json:"sessionCapabilities"`
					} `json:"agentCapabilities"`
				}
				if json.Unmarshal(message.Result, &init) != nil || init.ProtocolVersion != 1 {
					protocolErr = errors.New("unsupported ACP protocol version")
					s.Stop()
					continue
				}
				params := map[string]any{"cwd": req.WorkDir, "mcpServers": []any{}}
				method := "session/new"
				if req.ResumeSession != "" {
					if _, ok := init.AgentCapabilities.Sessions["resume"]; !ok {
						protocolErr = errors.New("ACP server does not advertise resume")
						s.Stop()
						continue
					}
					method = "session/resume"
					params["sessionId"] = req.ResumeSession
					result.SessionID = req.ResumeSession
				}
				expectedID = 2
				if err := write(2, method, params); err != nil {
					protocolErr = err
					s.Stop()
				}
			case 2:
				if req.ResumeSession == "" {
					var created struct {
						SessionID string `json:"sessionId"`
					}
					if json.Unmarshal(message.Result, &created) != nil || created.SessionID == "" {
						protocolErr = ErrProtocol
						s.Stop()
						continue
					}
					result.SessionID = created.SessionID
				}
				expectedID = 3
				if err := write(3, "session/prompt", map[string]any{"sessionId": result.SessionID, "prompt": []map[string]string{{"type": "text", "text": req.Prompt}}}); err != nil {
					protocolErr = err
					s.Stop()
				}
			case 3:
				var final struct {
					StopReason string `json:"stopReason"`
				}
				if json.Unmarshal(message.Result, &final) != nil || final.StopReason != "end_turn" {
					protocolErr = errors.New("ACP prompt did not complete normally")
				} else {
					complete = true
				}
				expectedID = 4
				if err := write(4, "session/close", map[string]string{"sessionId": result.SessionID}); err != nil {
					protocolErr = err
					s.Stop()
				}
			case 4:
				s.CloseInput() // server's disconnect teardown flushes persistence.
				// A non-exiting server cannot hold the allocation indefinitely.
				timer = time.NewTimer(2 * time.Second)
				stopTimer = timer.C
			}
		}
	}
}
