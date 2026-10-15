package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"

	"coordica/internal/process"
)

// JSONL drivers share process supervision and framing. The terminal schemas
// remain distinct: an unfamiliar event or prose never becomes completion.
func executeJSONL(ctx context.Context, def Definition, req Request, onEvent func(Event)) (Result, error) {
	s, err := process.Start(ctx, process.Spec{Executable: req.Executable, Args: launchArgs(def, req), Dir: req.WorkDir, Env: executionEnv(req)})
	if err != nil {
		return Result{ExitCode: -1}, err
	}
	emit(onEvent, "process.started", map[string]int{"pid": s.PID()})
	inputDone := make(chan error, 1)
	go func() { inputDone <- errors.Join(s.Write([]byte(req.Prompt)), s.CloseInput()) }()
	result := Result{ExitCode: -1}
	complete := false
	var protocolErr error
	for output := range s.Output() {
		if output.Stream != "stdout" || strings.TrimSpace(string(output.Line)) == "" {
			continue
		}
		var envelope struct {
			Type             string                      `json:"type"`
			ThreadID         string                      `json:"thread_id"`
			SessionID        string                      `json:"session_id"`
			Subtype          string                      `json:"subtype"`
			IsError          bool                        `json:"is_error"`
			Result           string                      `json:"result"`
			StructuredOutput json.RawMessage             `json:"structured_output"`
			Item             struct{ Type, Text string } `json:"item"`
		}
		if json.Unmarshal(output.Line, &envelope) != nil || envelope.Type == "" {
			protocolErr = ErrProtocol
			s.Stop()
			continue
		}
		onEvent(Event{Type: envelope.Type, Data: sanitize(output.Line)})
		switch def.Protocol {
		case ProtocolCodex:
			switch envelope.Type {
			case "thread.started":
				result.SessionID = envelope.ThreadID
			case "item.completed":
				if envelope.Item.Type == "agent_message" {
					result.OutputText = envelope.Item.Text
				}
			case "turn.completed":
				complete = true
			case "turn.failed":
				protocolErr = errors.New("JSONL turn failed")
				complete = false
				// Reconnection diagnostics are not terminal outcomes.
			}
		case ProtocolClaude:
			if envelope.SessionID != "" {
				result.SessionID = envelope.SessionID
			}
			if envelope.Type == "result" {
				if envelope.IsError || envelope.Subtype != "success" {
					protocolErr = errors.New("stream result failed or unknown")
				} else {
					if req.OutputSchemaPath != "" {
						structured := bytes.TrimSpace(envelope.StructuredOutput)
						if len(structured) == 0 || structured[0] != '{' {
							protocolErr = errors.New("Claude result omitted structured_output object")
							complete = false
						} else {
							complete = true
							result.OutputText = string(structured)
						}
					} else {
						complete = true
						result.OutputText = envelope.Result
					}
				}
			}
		}
		if len(result.OutputText) > 8<<20 {
			protocolErr = errors.New("agent result exceeds size limit")
			s.Stop()
		}
	}
	status, waitErr := s.Wait()
	result.ExitCode, result.TreeStopped = status.ExitCode, status.TreeStopped
	if !complete || result.SessionID == "" {
		protocolErr = errors.Join(protocolErr, ErrProtocol)
	}
	return result, errors.Join(waitErr, protocolErr, <-inputDone)
}

// Claude accepts an inline schema argument. The trusted caller supplies a file
// like Codex, but file I/O and protocol translation stay inside this adapter.
func readClaudeSchema(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, (64<<10)+1))
	if err != nil {
		return "", err
	}
	if len(b) > 64<<10 {
		return "", errors.New("bounded JSON object output schema required")
	}
	b = bytes.TrimSpace(b)
	if len(b) == 0 || b[0] != '{' || !json.Valid(b) {
		return "", errors.New("bounded JSON object output schema required")
	}
	return string(b), nil
}
