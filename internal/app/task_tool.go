package app

import (
	"context"
	"encoding/json"
	"fmt"

	"coordica/internal/domain"
	"coordica/internal/store"
)

// ChangeTaskTool preserves the existing work item and its approval boundary.
// Keeping this focused use case outside Service avoids growing its receiver.
func ChangeTaskTool(ctx context.Context, s *Service, a Actor, key, id string, in domain.TaskToolRequest) (json.RawMessage, error) {
	if err := owner(a); err != nil {
		return nil, err
	}
	if err := domain.ValidateToolID(in.Tool); err != nil {
		return nil, err
	}
	if !domain.ChatText(in.Reason, 2048, true) {
		return nil, domain.Fail("invalid_input", "a bounded selection reason is required")
	}
	return s.mutate(ctx, a, key, "task.tool."+id, in, func(tx *store.Tx) (any, error) {
		var task domain.Task
		if err := tx.Get("task", id, &task); err != nil {
			return nil, err
		}
		if err := version(task.Version, in.ExpectedVersion); err != nil {
			return nil, err
		}
		if (task.State != "draft" && task.State != "ready") || task.Generation != 0 ||
			task.UsedSeconds != 0 || task.ReservedSeconds != 0 || task.CurrentRunID != "" || task.CurrentCandidateID != "" || task.Verification != nil {
			return nil, domain.Fail("conflict", "execution tool can only change before the task's first Run")
		}
		runs, err := store.List[domain.Run](tx, "run")
		if err != nil {
			return nil, err
		}
		for _, run := range runs {
			if run.TaskID == task.ID {
				return nil, domain.Fail("conflict", "a previously issued task cannot be retargeted")
			}
		}
		if err = selectableTaskTool(tx, in.Tool); err != nil {
			return nil, err
		}
		old := task.Tool
		if old != in.Tool {
			task.Tool = in.Tool
			task.Version++
			task.AuthorizationVersion++
			// Existing content approval remains valid; selecting a tool never
			// creates approval for a draft or clears any pause/goal hold.
			if err = tx.Put("task", task.ID, task); err != nil {
				return nil, err
			}
		}
		return task, s.audit(tx, a, "task.tool_selected", task.ID, fmt.Sprintf("%s -> %s; %s", old, in.Tool, in.Reason))
	})
}

func selectableTaskTool(tx *store.Tx, tool string) error {
	settings, err := automation(tx)
	if err != nil {
		return err
	}
	nodes, err := store.List[domain.Node](tx, "node")
	if err != nil {
		return err
	}
	for _, node := range nodes {
		if !nodeAllows(node, tool, "implement") || !available(node.Capabilities, tool) {
			continue
		}
		if _, _, valid := taskExecution(node, tool, settings); valid {
			return nil
		}
	}
	return domain.Fail("developer_unavailable", "no paired Runner reports a compatible enabled development tool/selection")
}
