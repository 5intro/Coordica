package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"coordica/internal/domain"
	"coordica/internal/store"
)

// nodeConsoleActor checks the authenticated node again in the transaction so
// a stale Actor cannot read or write after its node has been revoked.
func nodeConsoleActor(tx *store.Tx, a Actor) (domain.Node, error) {
	var node domain.Node
	if a.Role != "node" || a.NodeID == "" || a.ID != a.NodeID {
		return node, domain.Fail("forbidden", "需要本机节点权限")
	}
	if err := tx.Get("node", a.NodeID, &node); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return node, domain.Fail("forbidden", "节点不存在")
		}
		return node, err
	}
	if node.Revoked {
		return node, domain.Fail("forbidden", "节点已撤销")
	}
	node.TokenHash = ""
	return node, nil
}

// NodeSnapshot exposes only this node's work and requirement proposals. It
// never derives its response from the owner snapshot or its audit event feed.
func (s *Service) NodeSnapshot(ctx context.Context, a Actor) (domain.NodeSnapshot, error) {
	result := domain.NodeSnapshot{APIVersion: domain.APIVersion, Tasks: []domain.Task{}, Runs: []domain.Run{}, Requirements: []domain.Requirement{}}
	err := s.DB.View(ctx, func(tx *store.Tx) error {
		var err error
		if result.Node, err = nodeConsoleActor(tx, a); err != nil {
			return err
		}
		project, err := getProject(tx)
		if err == nil {
			result.ProjectName = project.Name
			result.ProjectPaused = project.Paused
		} else if !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		runs, err := store.List[domain.Run](tx, "run")
		if err != nil {
			return err
		}
		taskIDs := make(map[string]bool)
		for _, run := range runs {
			if run.NodeID == a.NodeID {
				result.Runs = append(result.Runs, run)
				taskIDs[run.TaskID] = true
			}
		}
		tasks, err := store.List[domain.Task](tx, "task")
		if err != nil {
			return err
		}
		for _, task := range tasks {
			if taskIDs[task.ID] {
				result.Tasks = append(result.Tasks, task)
			}
		}
		requirements, err := store.List[domain.Requirement](tx, "requirement")
		if err != nil {
			return err
		}
		for _, requirement := range requirements {
			if requirement.SourceNodeID == a.NodeID {
				result.Requirements = append(result.Requirements, requirement)
			}
		}
		return nil
	})
	if err != nil {
		return domain.NodeSnapshot{}, err
	}
	return result, nil
}

// SubmitRequirement records a proposal for owner consideration. It does not
// create a task, grant an execution lease, or approve any work.
func (s *Service) SubmitRequirement(ctx context.Context, a Actor, key string, proposal domain.RequirementProposal) (json.RawMessage, error) {
	if a.Role != "node" || a.NodeID == "" || a.ID != a.NodeID {
		return nil, domain.Fail("forbidden", "需要本机节点权限")
	}
	proposal.Title = strings.TrimSpace(proposal.Title)
	proposal.Body = strings.TrimSpace(proposal.Body)
	if err := domain.ValidateRequirementProposal(proposal); err != nil {
		return nil, err
	}
	return s.mutateWithCheck(ctx, a, key, "requirement.submit", proposal, func(tx *store.Tx) error {
		_, err := nodeConsoleActor(tx, a)
		return err
	}, func(tx *store.Tx) (any, error) {
		return s.createRequirement(tx, a, domain.Requirement{Title: proposal.Title, Body: proposal.Body, SourceNodeID: a.NodeID})
	})
}
