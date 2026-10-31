package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"coordica/internal/domain"
	"coordica/internal/gitops"
	"coordica/internal/store"
)

func mergedExternalFixture(t *testing.T, path, mode string) (*itFixture, *testPullHost) {
	t.Helper()
	f, h := hostedFixture(t)
	ctx := context.Background()
	if err := f.s.DB.Update(ctx, func(tx *store.Tx) error {
		p, err := getProject(tx)
		if err != nil {
			return err
		}
		p.Publication.ReviewProviders = []domain.ExternalReviewPolicy{{Provider: "codex", ActorID: 42, AppID: 99, CheckNames: map[string]string{"security_review": "Codex security"}, Kinds: []string{"security_review"}}}
		return tx.Put("project", p.ID, p)
	}); err != nil {
		t.Fatal(err)
	}
	h.external = mergedExternalReport("success")
	f.implement(t, "src/feature.txt")
	if err := f.w.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	f.reviewAndValidate(t)
	refreshHosted(t, f) // Publish the original green with pending CI, without merging.
	if h.reviewCheck != "success" || h.merges != 0 {
		t.Fatal("fixture did not establish the original green status")
	}
	c := f.candidate(t)
	if err := f.s.DB.Update(ctx, func(tx *store.Tx) error {
		if path == "cached" {
			c.PullRequest.MergeReady, c.PullRequest.CI = true, "success"
			var b pullRequestBinding
			if err := tx.Get("pull_request", c.TaskID, &b); err != nil {
				return err
			}
			b.NextSync = f.s.Now().Add(time.Hour)
			if err := tx.Put("pull_request", c.TaskID, b); err != nil {
				return err
			}
		} else if path == "intent" {
			c.State = "landing"
			in := domain.Intent{ID: "external-merge-intent", CandidateID: c.ID, Expected: c.Main, Desired: c.Commit,
				State: "unknown", CreatedAt: f.s.Now(), PublicationMode: "github", PullRequestNumber: 7}
			if err := tx.Put("intent", in.ID, in); err != nil {
				return err
			}
		}
		if err := tx.Put("candidate", c.ID, c); err != nil {
			return err
		}
		return tx.Put("resource", "provider-gate-lock", domain.Resource{ID: "provider-gate-lock", TaskID: c.TaskID})
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.g.Publish(ctx, f.w.repo(), f.remote, "refs/heads/main", gitops.OID(c.Main), gitops.OID(c.Commit)); err != nil {
		t.Fatal(err)
	}
	h.merged, h.ci, h.external = true, "success", mergedExternalReport(mode)
	return f, h
}

func mergedExternalReport(state string) func(domain.PullRequestSpec) []domain.ExternalReview {
	return mergedExternalReportAt(state, 20)
}

func mergedExternalReportAt(state string, checkID int64) func(domain.PullRequestSpec) []domain.ExternalReview {
	return func(spec domain.PullRequestSpec) []domain.ExternalReview {
		if state == "missing" {
			return nil
		}
		return []domain.ExternalReview{stampExternalReport(domain.ExternalReview{Provider: "codex", Kind: "security_review", ActorID: 42, AppID: 99,
			CheckID: checkID, CheckName: "Codex security", ReportID: 10, Commit: spec.Head, State: state, Summary: "provider result", URL: "https://github.com/owner/repo/pull/7"})}
	}
}

func TestExternalReviewMergedPRCannotBypassGateOrRecovery(t *testing.T) {
	for _, path := range []string{"sync", "cached", "intent"} {
		for _, mode := range []string{"missing", "pending", "failure", "success"} {
			t.Run(path+"/"+mode, func(t *testing.T) {
				f, h := mergedExternalFixture(t, path, mode)
				ctx := context.Background()
				before := f.snapshot(t).Tasks[0]
				if path == "sync" {
					refreshHosted(t, f)
				} else if err := f.w.Tick(ctx); err != nil {
					t.Fatal(err)
				}
				resourceExists := func() bool {
					err := f.s.DB.View(ctx, func(tx *store.Tx) error {
						var resource domain.Resource
						return tx.Get("resource", "provider-gate-lock", &resource)
					})
					if err != nil && !errors.Is(err, domain.ErrNotFound) {
						t.Fatal(err)
					}
					return err == nil
				}
				if mode != "success" {
					after := f.snapshot(t).Tasks[0]
					if after.State == "integrated" || f.candidate(t).State == "landed" || !resourceExists() ||
						after.Retries != before.Retries || after.Repairs != before.Repairs {
						t.Fatal("merged flag bypassed current provider evidence or spent repair counts", after, f.candidate(t))
					}
					h.external = mergedExternalReportAt("success", 21)
					f.w = &Integrator{Service: f.s, Git: f.g, PullRequests: h}
					if path == "intent" {
						if err := f.w.Tick(ctx); err != nil {
							t.Fatal(err)
						}
					} else {
						refreshHosted(t, f)
					}
				}
				if f.candidate(t).State != "landed" || f.snapshot(t).Tasks[0].State != "integrated" || resourceExists() || h.merges != 0 {
					t.Fatal("fresh provider proof did not acknowledge the existing merge exactly once", f.candidate(t), h.merges)
				}
			})
		}
	}
}
