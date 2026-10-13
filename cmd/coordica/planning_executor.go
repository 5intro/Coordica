package main

import (
	"context"
	"coordica/internal/agent"
	"coordica/internal/app"
	_ "embed"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type CodexPlanningExecutor struct{ Executable string }

//go:embed leader.schema.json
var leaderOutputSchema []byte

//go:embed consult.schema.json
var consultOutputSchema []byte

func (e CodexPlanningExecutor) Execute(ctx context.Context, dir, prompt string, onStarted func(int) error) (app.PlanningOutcome, error) {
	scratch, err := os.MkdirTemp("", "coordica-leader-schema-")
	if err != nil {
		return app.PlanningOutcome{TreeStopped: true, Error: planningError(err)}, err
	}
	defer os.RemoveAll(scratch)
	schemaPath := filepath.Join(scratch, "leader.schema.json")
	schema := leaderOutputSchema
	if strings.HasPrefix(prompt, "COORDICA_ADVISORY_V1\n") {
		schema = consultOutputSchema
	}
	if err = os.WriteFile(schemaPath, schema, 0600); err != nil {
		return app.PlanningOutcome{TreeStopped: true, Error: planningError(err)}, err
	}
	execCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var persistenceErr error
	processStarted := false
	started := time.Now()
	r, err := agent.Execute(execCtx, agent.Request{Tool: "codex", Executable: e.Executable, WorkDir: dir, Prompt: prompt, Role: "leader", OutputSchemaPath: schemaPath}, func(event agent.Event) {
		if event.Type == "process.started" {
			processStarted = true
			var v struct {
				PID int `json:"pid"`
			}
			if json.Unmarshal(event.Data, &v) != nil || v.PID <= 0 {
				persistenceErr = errors.New("invalid process-start fact")
			} else if onStarted != nil {
				persistenceErr = onStarted(v.PID)
			}
			if persistenceErr != nil {
				cancel()
			}
		}
	})
	err = errors.Join(err, persistenceErr)
	return app.PlanningOutcome{Output: r.OutputText, SessionID: r.SessionID, TreeStopped: r.TreeStopped || !processStarted, ElapsedSeconds: int64(time.Since(started)/time.Second) + 1, Error: planningError(err)}, err
}

func planningError(err error) string {
	if err == nil {
		return ""
	}
	detail := err.Error()
	if len(detail) > 4096 {
		detail = detail[:4096]
	}
	return detail
}
