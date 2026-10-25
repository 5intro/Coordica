package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"coordica/internal/agent"
	"coordica/internal/domain"
	"coordica/internal/gitops"
	"coordica/internal/process"
)

func (r *Runner) perform(parent context.Context, record spool, clock responseClock) {
	assignment := record.Assignment
	completion := domain.CompleteRequest{Generation: assignment.Run.Generation, Status: "failed", Quiescent: true}
	var evidenceLog *evidence
	var outputText string
	finish := func(err error) {
		if errors.Is(err, process.ErrTreeNotStopped) {
			completion.Quiescent = false
			if errors.Is(err, gitops.ErrGit) || errors.Is(err, gitops.ErrOutputLimit) {
				// The recorded PID belongs to the Agent, not a later Git command.
				// Never use its dead group as evidence for an uncertain Git tree.
				record.PID = 0
			}
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			completion.Status = "stopped"
		}
		completion.ElapsedSeconds = int64(time.Since(record.StartedAt) / time.Second)
		if err != nil {
			completion.Detail = shortDetail(err)
			if strings.TrimSpace(outputText) != "" {
				text, cut := boundedText(r.safeText(outputText), 2048)
				completion.Detail += "; agent output: " + text
				if cut {
					completion.Detail += " [truncated]"
				}
			}
			completion.Detail = r.safeText(completion.Detail)
			r.report("run.failed", assignment.Run.ID, completion.Detail)
		}
		if evidenceLog != nil {
			evidenceLog.value("runner.completion", completion, true)
			if logErr := evidenceLog.close(); logErr != nil {
				if completion.Status != "stopped" {
					completion.Status = "failed"
				}
				completion.Detail = r.safeText("local evidence persistence failed: " + shortDetail(logErr))
				r.report("evidence.failed", assignment.Run.ID, completion.Detail)
			}
		}
		if record.Intent != nil {
			// Receipt replay must precede Complete, including after a restart.
			if record.Intent.Settled {
				completion.Detail = record.Intent.Completion.Detail
				record.Request = &completion
			} else {
				record.Intent.Completion = completion
			}
		} else {
			record.Request = &completion
		}
		if err := writePrivateJSON(r.spoolPath(assignment.Run.ID), record); err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := r.deliver(ctx, record); err != nil {
				r.report("handoff.pending", assignment.Run.ID, shortDetail(err))
			}
		} else {
			r.report("handoff.persistence_failed", assignment.Run.ID, shortDetail(err))
		}
	}
	lease, err := localDeadline(assignment.Run.LeaseUntil, clock)
	if err != nil {
		finish(err)
		return
	}
	deadline, err := localDeadline(assignment.Run.Deadline, clock)
	if err != nil {
		finish(err)
		return
	}
	ctx, stop := context.WithCancel(parent)
	defer stop()
	stopHeartbeat, permissionUntil := r.watchPermission(ctx, stop, assignment, record.StartedAt, lease, deadline, clock.Received)
	defer stopHeartbeat()
	base, err := gitops.ParseOID(assignment.Run.Base)
	if err != nil {
		finish(err)
		return
	}
	dir := filepath.Dir(record.WorkDir)
	if err = os.MkdirAll(dir, 0700); err != nil {
		finish(err)
		return
	}
	evidenceLog, err = newEvidence(filepath.Join(dir, "events.jsonl"), r.config.Token)
	if err != nil {
		finish(err)
		return
	}
	input := filepath.Join(dir, "input.bundle")
	file, err := os.OpenFile(input, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		finish(err)
		return
	}
	_, err = r.request(ctx, "GET", assignment.InputURL, "", nil, "", file)
	if syncErr := file.Sync(); err == nil {
		err = syncErr
	}
	file.Close()
	if err != nil {
		finish(err)
		return
	}
	if err = r.git.CloneSnapshot(ctx, input, base, record.WorkDir); err != nil {
		finish(err)
		return
	}
	definition, registered := r.registry.Resolve(assignment.Run.Tool)
	if !registered {
		finish(errors.New("assignment tool is not locally registered"))
		return
	}
	prompt := assignmentPrompt(assignment, definition.CommitMode)
	if err := domain.ValidateAssignmentContext(assignment); err != nil {
		finish(err)
		return
	}
	if len(prompt) > maxAssignmentPromptBytes {
		finish(errors.New("complete assignment context exceeds 256 KiB; split the task without dropping constraints"))
		return
	}
	if assignment.Run.Kind == "review" {
		reviewInput, err := r.readReviewInput(ctx, assignment, record.WorkDir)
		if err != nil {
			var cause error
			completion.Status, completion.Review, cause = reviewInputFailure(assignment.Run.Base, err, ctx.Err())
			if cause != nil {
				finish(cause)
				return
			}
			completion.Detail = "review blocked before agent execution: " + shortDetail(err)
			r.report("review.blocked", assignment.Run.ID, completion.Detail)
			finish(nil)
			return
		}
		prompt += reviewInput
	}
	request := agent.Request{Tool: assignment.Run.Tool, Executable: r.config.Executables[assignment.Run.Tool], WorkDir: record.WorkDir, Prompt: prompt, Role: assignment.Run.Kind, AllowedTools: []string{"Read", "Edit", "Write", "Glob", "Grep", "Bash"}, Model: assignment.Run.Model, ReasoningEffort: assignment.Run.ReasoningEffort}
	nativeIntent := assignment.GoalContext != nil && assignment.Run.Kind == "implement" && definition.Protocol == agent.ProtocolCodex && definition.ID == "codex"
	if nativeIntent {
		schemaPath := filepath.Join(dir, "execution.schema.json")
		if err = os.WriteFile(schemaPath, executionOutputSchema, 0600); err != nil {
			finish(err)
			return
		}
		request.OutputSchemaPath = schemaPath
		request.Prompt += nativeIntentPrompt
	}
	if assignment.Run.Kind == "review" {
		request.AllowedTools = []string{"Read", "Glob", "Grep"}
		if definition.Protocol == agent.ProtocolCodex || definition.Protocol == agent.ProtocolClaude {
			schemaPath := filepath.Join(dir, "review.schema.json")
			if err = os.WriteFile(schemaPath, reviewOutputSchema(assignment.Run.Base), 0600); err != nil {
				finish(err)
				return
			}
			request.OutputSchemaPath = schemaPath
		}
	}
	started := false
	var persistErr error
	result, err := r.execute(ctx, request, func(event agent.Event) {
		evidenceLog.append(event, false)
		if evidenceLog.err != nil {
			persistErr = evidenceLog.err
			stop()
		}
		if event.Type == "process.started" {
			started = true
			var identity struct {
				PID int `json:"pid"`
			}
			if json.Unmarshal(event.Data, &identity) != nil || identity.PID <= 0 {
				persistErr = errors.New("invalid process identity")
				stop()
				return
			}
			record.PID = identity.PID
			if e := writePrivateJSON(r.spoolPath(assignment.Run.ID), record); e != nil {
				persistErr = e
				stop()
			}
		}
	})
	outputText = result.OutputText
	loggedOutput, outputTruncated := boundedText(result.OutputText, 64<<10)
	evidenceLog.value("runner.result", struct {
		SessionID       string `json:"session_id"`
		OutputText      string `json:"output_text"`
		OutputTruncated bool   `json:"output_truncated"`
		ExitCode        int    `json:"exit_code"`
		TreeStopped     bool   `json:"tree_stopped"`
	}{result.SessionID, loggedOutput, outputTruncated, result.ExitCode, result.TreeStopped}, true)
	completion.SessionID = result.SessionID
	completion.Quiescent = result.TreeStopped || !started
	if nativeIntent && err == nil {
		// A successful native outcome needs an affirmative tree-stop receipt;
		// an absent process-start event alone cannot prove it.
		completion.Quiescent = result.TreeStopped
	}
	if persistErr != nil {
		err = errors.Join(err, persistErr)
	}
	if err != nil {
		finish(err)
		return
	}
	if !completion.Quiescent {
		finish(errors.New("managed process tree is not confirmed stopped"))
		return
	}
	if ctx.Err() != nil {
		completion.Status = "stopped"
		finish(ctx.Err())
		return
	}
	if nativeIntent {
		if !result.TreeStopped || result.ExitCode != 0 {
			finish(errors.New("native execution lacks a successful stopped protocol result"))
			return
		}
		intent, parseErr := domain.ParseExecutionIntent(result.OutputText)
		if parseErr != nil {
			finish(parseErr)
			return
		}
		if intent.Status != "completed" {
			// Never convert a scope/block intent to an ordinary repair failure.
			completion.Status = "stopped"
			completion.IntentStatus = intent.Status
			if intent.Status == "scope_requested" {
				if err = r.git.VerifyClean(ctx, record.WorkDir, base); err != nil {
					finish(fmt.Errorf("native scope/block intent requires unchanged fixed workspace: %w", err))
					return
				}
				if _, err = gitops.NewScope(intent.RequestedPaths, nil); err != nil {
					finish(err)
					return
				}
			}
			if ctx.Err() != nil {
				finish(ctx.Err())
				return
			}
			completion.Detail = "native " + intent.Status + " intent; actual process tree stopped; fixed workspace unchanged"
			if intent.Status == "blocked" {
				completion.Detail = "native blocked intent; actual process tree stopped; workspace preserved without snapshot"
			}
			completion.ElapsedSeconds = int64(time.Since(record.StartedAt) / time.Second)
			record.Intent = &intentHandoff{Intent: intent, Key: intentKey(assignment.Run), Completion: completion}
			if err = writePrivateJSON(r.spoolPath(assignment.Run.ID), record); err != nil {
				finish(err)
				return
			}
			err = r.attemptIntent(ctx, &record)
			finish(err)
			return
		}
	}
	var head gitops.OID
	if assignment.Run.Kind == "implement" && definition.CommitMode == "runner" {
		scope, scopeErr := gitops.NewScope(assignment.Task.AllowedPaths, assignment.Project.ProtectedPaths)
		if scopeErr != nil {
			finish(scopeErr)
			return
		}
		head, err = r.git.CommitScoped(ctx, record.WorkDir, base, scope, "Coordica: Runner snapshot "+assignment.Run.ID, gitops.Identity{Name: "Coordica Runner", Email: "runner@coordica.local"})
	} else {
		head, err = r.git.Head(ctx, record.WorkDir)
	}
	if err != nil {
		finish(err)
		return
	}
	if err = r.git.VerifyClean(ctx, record.WorkDir, head); err != nil {
		finish(fmt.Errorf("agent left uncommitted or unexpected workspace changes: %w", err))
		return
	}
	if assignment.Run.Kind == "review" {
		if head != base {
			finish(errors.New("reviewer changed the fixed candidate"))
			return
		}
		review, err := parseReview(result.OutputText, assignment.Run.Base)
		if err != nil {
			finish(err)
			return
		}
		completion.Review = &review
		completion.Review.Summary = r.safeText(completion.Review.Summary)
		for i, finding := range completion.Review.Findings {
			completion.Review.Findings[i] = r.safeText(finding)
		}
	} else {
		if head == base {
			finish(errors.New("implementation produced no committed change"))
			return
		}
		bundlePath := filepath.Join(dir, "result.bundle")
		if _, err = r.git.CreateBundle(ctx, record.WorkDir, base, head, bundlePath); err != nil {
			finish(err)
			return
		}
		// Freeze the latest confirmed window at delivery start; later heartbeats
		// cannot extend this upload's finite retry deadline.
		artifact, err := uploadArtifact(ctx, r, assignment.Run.ID, record.Key+"-artifact", bundlePath, permissionUntil())
		if err != nil {
			finish(err)
			return
		}
		completion.Head, completion.ArtifactID = head.String(), artifact.ID
	}
	if ctx.Err() != nil {
		finish(ctx.Err())
		return
	}
	completion.Status = "succeeded"
	completion.Detail = "structured tool result and fixed workspace verified"
	finish(nil)
}

const nativeIntentPrompt = `
Native execution intent protocol v1: return ONLY a JSON object matching the supplied output schema with EXACT fields version=1, status, requested_paths (non-null string array), reason (string), summary (nonempty string). No Markdown or prose outside JSON.
If task paths are insufficient, STOP before editing any files and return status="scope_requested", the exact additional repository paths in requested_paths, and a nonempty reason. Leave the fixed workspace unchanged. The Runner will submit your request after your actual process tree stops; it grants no permission. Wait for a new assignment with extended authority before implementing. Do not call HTTP or configure MCP yourself.
For major changes or other blockers, leave the fixed workspace unchanged and return status="blocked", requested_paths=[], and a nonempty reason. Never approve or carry out a major change yourself.
When implementation and relevant checks are finished within current scope, return status="completed", requested_paths=[], reason="", and a summary. This is intent only: Runner still verifies actual changes, scope, fixed base, and process quiescence before snapshotting.
`

func parseReview(text, candidate string) (domain.Review, error) {
	var review domain.Review
	decoder := json.NewDecoder(bytes.NewBufferString(text))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&review); err != nil {
		return review, fmt.Errorf("review is not strict JSON: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return review, errors.New("review has trailing or invalid content")
	}
	if review.Candidate != candidate || review.Summary == "" || review.Findings == nil {
		return review, errors.New("review is missing fields or names another candidate")
	}
	if review.Verdict != "approve" && review.Verdict != "changes_requested" && review.Verdict != "blocked" {
		return review, errors.New("unknown review verdict")
	}
	if review.RunID != "" {
		return review, errors.New("review cannot assert a Run identity")
	}
	return review, nil
}
