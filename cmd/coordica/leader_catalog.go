package main

import (
	"context"
	"errors"

	"coordica/internal/domain"
	"coordica/internal/runner"
)

func newLeaderExecutor(codex, toolsFile string, overrides executableFlags) (*AgentPlanningExecutor, error) {
	definitions, err := loadToolDefinitions(toolsFile)
	if err != nil {
		return nil, err
	}
	registry, err := runner.LocalRegistry(definitions)
	if err != nil {
		return nil, err
	}
	if overrides == nil {
		overrides = executableFlags{}
	}
	if codex != "" {
		if chosen := overrides["codex"]; chosen != "" && chosen != codex {
			return nil, errors.New("conflicting Codex executable overrides")
		}
		overrides["codex"] = codex
	}
	paths, err := runner.NormalizeExecutables(registry, overrides)
	if err != nil {
		return nil, err
	}
	return &AgentPlanningExecutor{Registry: registry, Executables: paths}, nil
}

// Discovery is local, demand-driven and bounded; registry caches model catalogs
// rather than opening an agent conversation for each UI read or task poll.
func (e *AgentPlanningExecutor) DiscoverLeaders(parent context.Context) ([]domain.Capability, error) {
	result := []domain.Capability{}
	for _, cap := range e.Registry.Discover(parent, e.Executables) {
		if !cap.Installed {
			continue
		}
		item := domain.Capability{Tool: cap.Tool, Version: cap.Version, Installed: cap.Installed,
			Available: cap.Available, Structured: cap.Structured, Protocol: cap.Protocol,
			Review: cap.Review, ReadOnly: cap.ReadOnly, Resume: cap.Resume, Cancel: cap.Cancel,
			ModelDiscovery: cap.ModelDiscovery, Detail: cap.Detail, Models: []domain.ModelOption{}}
		for _, model := range cap.Models {
			item.Models = append(item.Models, domain.ModelOption{ID: model.ID, Name: model.Name,
				ReasoningEfforts: append([]string{}, model.ReasoningEfforts...), DefaultReasoningEffort: model.DefaultReasoningEffort})
		}
		result = append(result, item)
	}
	if parent.Err() != nil {
		return nil, parent.Err()
	}
	return result, nil
}
