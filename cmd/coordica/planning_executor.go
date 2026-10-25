package main

import (
	"context"
	"coordica/internal/agent"
	"coordica/internal/app"
	"coordica/internal/domain"
	"coordica/internal/process"
	_ "embed"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type CodexPlanningExecutor struct{ Executable string }

// Registry and executable overrides are trusted center-host configuration. A
// persisted selection contains only tool/model identifiers, never launch argv.
type AgentPlanningExecutor struct {
	Registry    *agent.Registry
	Executables map[string]string
}

//go:embed leader.schema.json
var leaderOutputSchema []byte

//go:embed initialization.schema.json
var initializationOutputSchema []byte

//go:embed consult.schema.json
var consultOutputSchema []byte

//go:embed requirement-change.schema.json
var requirementOutputSchema []byte

func (e CodexPlanningExecutor) Execute(ctx context.Context, dir, prompt string, onStarted func(int) error) (app.PlanningOutcome, error) {
	return (&AgentPlanningExecutor{Registry: agent.DefaultRegistry(), Executables: map[string]string{"codex": e.Executable}}).
		ExecuteSelected(ctx, domain.DefaultLeaderSelection(), dir, prompt, onStarted)
}

func (e *AgentPlanningExecutor) Execute(ctx context.Context, dir, prompt string, onStarted func(int) error) (app.PlanningOutcome, error) {
	return e.ExecuteSelected(ctx, domain.DefaultLeaderSelection(), dir, prompt, onStarted)
}

func (e *AgentPlanningExecutor) ExecuteSelected(ctx context.Context, selection domain.LeaderSelection,
	dir, prompt string, onStarted func(int) error,
) (app.PlanningOutcome, error) {
	if err := domain.ValidateLeaderSelection(selection); err != nil {
		return app.PlanningOutcome{TreeStopped: true}, err
	}
	def, ok := e.Registry.Resolve(selection.Tool)
	if !ok || !def.Review || !def.Cancel {
		return app.PlanningOutcome{TreeStopped: true}, errors.New("selected Leader protocol is unavailable")
	}
	scratch, err := os.MkdirTemp("", "coordica-leader-schema-")
	if err != nil {
		return app.PlanningOutcome{TreeStopped: true, Error: planningError(err)}, err
	}
	defer os.RemoveAll(scratch)
	schemaPath := filepath.Join(scratch, "leader.schema.json")
	schema := leaderOutputSchema
	if strings.HasPrefix(prompt, "COORDICA_INITIALIZATION_V1\n") {
		schema = initializationOutputSchema
	}
	if strings.HasPrefix(prompt, "COORDICA_ADVISORY_V1\n") {
		schema = consultOutputSchema
	}
	if strings.HasPrefix(prompt, "COORDICA_REQUIREMENT_CHANGE_V1\n") {
		schema = requirementOutputSchema
	}
	if err = os.WriteFile(schemaPath, schema, 0600); err != nil {
		return app.PlanningOutcome{TreeStopped: true, Error: planningError(err)}, err
	}
	req := agent.Request{Tool: selection.Tool, Model: selection.Model, ReasoningEffort: selection.ReasoningEffort,
		Executable: e.Executables[selection.Tool], WorkDir: dir, Prompt: prompt, Role: "leader"}
	if def.Protocol == agent.ProtocolCodex || def.Protocol == agent.ProtocolClaude {
		req.OutputSchemaPath = schemaPath
	} else {
		// These drivers do not advertise native JSON-schema enforcement. The
		// application still strictly decodes and validates the returned result.
		req.Prompt += "\nReturn exactly one JSON object matching this schema, without Markdown or extra prose:\n" + string(schema)
	}
	execCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var persistenceErr error
	processStarted := false
	started := time.Now()
	r, err := e.Registry.Execute(execCtx, req, func(event agent.Event) {
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
		if activity, ok := agent.LeaderPublicActivity(event); ok {
			app.ReportLeaderActivity(execCtx, domain.LeaderActivitySignal{Key: activity.Key, Kind: activity.Kind, Text: activity.Text})
		}
	})
	err = errors.Join(err, persistenceErr)
	if err == nil && r.OutputText != "" {
		app.ReportLeaderActivity(execCtx, domain.LeaderActivitySignal{Key: "leader-result", Kind: "result_received",
			Text: "结构化结果已返回，等待中心核对"})
	}
	stopped := r.TreeStopped || !processStarted && !errors.Is(err, process.ErrTreeNotStopped)
	return app.PlanningOutcome{Output: r.OutputText, SessionID: r.SessionID, TreeStopped: stopped,
		ElapsedSeconds: int64(time.Since(started)/time.Second) + 1, Error: planningError(err)}, err
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
