package app

import (
	"context"
	"encoding/json"
	"errors"

	"coordica/internal/domain"
	"coordica/internal/store"
)

// The center supplies its own local discovery, never a remote Runner report.
// Tool/process I/O completes before any configuration transaction starts.
type LeaderCatalog interface {
	DiscoverLeaders(context.Context) ([]domain.Capability, error)
}

type LeaderConfigurations struct{ Service *Service }

func leaderSelection(settings domain.AutomationSettings) domain.LeaderSelection {
	return domain.LeaderSelection{Tool: settings.LeaderTool, Model: settings.LeaderModel,
		ReasoningEffort: settings.LeaderReasoningEffort}
}

func leaderChoice(settings domain.AutomationSettings) *domain.LeaderSelection {
	choice := leaderSelection(settings)
	return &choice
}

func pinnedLeader(selection *domain.LeaderSelection) domain.LeaderSelection {
	if selection == nil {
		return domain.DefaultLeaderSelection() // Legacy records retain their original native model.
	}
	return *selection
}

func (c LeaderConfigurations) Read(ctx context.Context, a Actor) (domain.LeaderConfiguration, error) {
	s := c.Service
	var result domain.LeaderConfiguration
	if err := owner(a); err != nil {
		return result, err
	}
	err := s.DB.View(ctx, func(tx *store.Tx) error {
		var settings domain.AutomationSettings
		err := tx.Get("automation", "current", &settings)
		if errors.Is(err, domain.ErrNotFound) {
			settings, err = domain.DefaultAutomationSettings(), nil
		}
		result.Version, result.Selection = settings.Version, leaderSelection(settings)
		return err
	})
	if err != nil {
		return result, err
	}
	result.Capabilities = []domain.Capability{}
	if s.LeaderCatalog != nil {
		result.Capabilities, err = s.LeaderCatalog.DiscoverLeaders(ctx)
	}
	if result.Capabilities == nil {
		result.Capabilities = []domain.Capability{}
	}
	result.QueriedAt = s.Now()
	return result, err
}

func (c LeaderConfigurations) validate(ctx context.Context, selection domain.LeaderSelection) error {
	s := c.Service
	if err := domain.ValidateLeaderSelection(selection); err != nil {
		return err
	}
	if s.LeaderCatalog == nil {
		return domain.Fail("leader_unavailable", "center-local Leader discovery is unavailable")
	}
	catalog, err := s.LeaderCatalog.DiscoverLeaders(ctx)
	if err != nil {
		return err
	}
	for _, capability := range catalog {
		if capability.Tool == selection.Tool && capability.Available && capability.Structured &&
			capability.Cancel && capability.Review {
			return domain.ValidateExecutionOptions(capability, selection.Model, selection.ReasoningEffort)
		}
	}
	return domain.Fail("leader_unavailable", "selected Leader tool is not available on this center host")
}

func (c LeaderConfigurations) Configure(ctx context.Context, a Actor, key string, req domain.LeaderConfigureRequest) (json.RawMessage, error) {
	s := c.Service
	if err := owner(a); err != nil {
		return nil, err
	}
	if raw, found, err := replayMutation(ctx, s, a, key, "leader.configure", req, nil); found || err != nil {
		return raw, err
	}
	if err := c.validate(ctx, req.Selection); err != nil {
		return nil, err
	}
	return s.mutate(ctx, a, key, "leader.configure", req, func(tx *store.Tx) (any, error) {
		return c.apply(tx, a, req)
	})
}

func (c LeaderConfigurations) apply(tx *store.Tx, a Actor, req domain.LeaderConfigureRequest) (domain.AutomationSettings, error) {
	s := c.Service
	settings, err := automation(tx)
	if err != nil {
		return settings, err
	}
	if err = version(settings.Version, req.ExpectedVersion); err != nil {
		return settings, err
	}
	if leaderSelection(settings) == req.Selection {
		return settings, nil
	}
	settings.LeaderTool, settings.LeaderModel = req.Selection.Tool, req.Selection.Model
	settings.LeaderReasoningEffort = req.Selection.ReasoningEffort
	settings.Version++
	if err = tx.Put("automation", "current", settings); err != nil {
		return settings, err
	}
	return settings, s.audit(tx, a, "leader.configured", "current", req.Selection.Tool)
}
