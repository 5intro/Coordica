package app

import (
	"context"
	"errors"

	"coordica/internal/domain"
	"coordica/internal/store"
)

var authorityPaths = []string{
	"AGENTS.md", "AGENT.md", "QUALITY.md", ".github/**", "cmd/coordica/**", "cmd/qualitycheck/**",
	"internal/app/**", "internal/domain/**", "internal/httpapi/**", "internal/localhttp/**",
	"internal/nodeconsole/**", "internal/gitops/**", "internal/github/**", "internal/process/**",
	"internal/store/**", "internal/agent/**", "internal/runner/**", "internal/onboarding/**",
	"internal/nodeservice/**", "internal/localfile/**", "internal/filelock/**", "internal/mcp/**",
	"internal/chatprofile/**", "internal/mcpconfig/**", "internal/centerhost/**", "internal/codequality/**",
	"scripts/check-quality.py", "api/**",
}

const authorityPolicyVersion = 3
const authorityMigrationDetail = "authority protection migration v3 requires explicit baseline initialization; no permissions granted"
const authorityMigrationAudit = "v3; initialization required; no grants"

// MigrateAuthorityPolicy applies the mandatory authority-protection
// migration. It preserves project identity/history, never grandfathering a
// baseline or grant. Active old executions are drained through existing stop
// requests, not erased or treated as stopped.
func (s *Service) MigrateAuthorityPolicy(ctx context.Context) error {
	return s.DB.Update(ctx, func(tx *store.Tx) error {
		var marker struct {
			Version int `json:"version"`
		}
		e := tx.Get("authority_policy_migration", "current", &marker)
		if e == nil && marker.Version >= authorityPolicyVersion {
			return nil
		}
		if e != nil && !errors.Is(e, domain.ErrNotFound) {
			return e
		}
		p, e := getProject(tx)
		if errors.Is(e, domain.ErrNotFound) {
			return nil
		}
		if e != nil {
			return e
		}
		seen := map[string]bool{}
		paths := []string{}
		for _, path := range p.ProtectedPaths {
			if !seen[path] {
				paths = append(paths, path)
				seen[path] = true
			}
		}
		for _, path := range authorityPaths {
			if !seen[path] {
				paths = append(paths, path)
				seen[path] = true
			}
		}
		// The marker versions authority semantics, independently of path content.
		// Even an Owner who pre-added every path must renew the old baseline.
		p.ProtectedPaths = paths
		p.PolicyVersion++
		p.Version++
		p.Paused = true
		if e = tx.Put("project", p.ID, p); e != nil {
			return e
		}
		if e = stopPlanning(tx); e != nil {
			return e
		}
		if e = requestStop(tx, ""); e != nil {
			return e
		}
		v, e := initialization(tx)
		if e != nil {
			return e
		}
		v.Status = "draft"
		v.ApprovedVersion = 0
		v.ApprovedDigest = ""
		v.ApprovedBaseline = nil
		v.Detail = authorityMigrationDetail
		if e = tx.Put("initialization", "current", v); e != nil {
			return e
		}
		tasks, e := store.List[domain.Task](tx, "task")
		if e != nil {
			return e
		}
		for _, t := range tasks {
			if domain.TaskFinished(t) {
				continue
			}
			t.ApprovedVersion = 0
			t.AuthorizationVersion++
			t.Version++
			t.HoldReason = "baseline_changed"
			if e = tx.Put("task", t.ID, t); e != nil {
				return e
			}
		}
		if e = tx.Put("authority_policy_migration", "current", struct {
			Version int `json:"version"`
		}{authorityPolicyVersion}); e != nil {
			return e
		}
		return s.audit(tx, Actor{ID: "center"}, "project.authority_policy_migrated", p.ID, authorityMigrationAudit)
	})
}
