package app

import (
	"context"
	"strings"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func TestExternalReviewFailureFeedbackRetainsAttributedExcerpt(t *testing.T) {
	facts := domain.PullRequestStatus{ExternalReviews: []domain.ExternalReview{{Kind: "security_review", State: "failure", Summary: "1 unresolved finding", URL: "https://github.com/owner/repo/pull/7", Evidence: "Raw provider excerpt: retry writes before idempotency lookup"}}}
	text := hostedFeedback(facts)
	if !strings.Contains(text, "retry writes before idempotency lookup") || !strings.Contains(text, facts.ExternalReviews[0].URL) {
		t.Fatal("repair lost original external evidence", text)
	}
	facts.ExternalReviews[0].State = "pending"
	if strings.Contains(hostedFeedback(facts), facts.ExternalReviews[0].Evidence) {
		t.Fatal("incomplete provider operation contributed code-failure evidence")
	}
}

func TestExternalReviewGateCannotBeBypassedByAdapterMergeReady(t *testing.T) {
	for _, mode := range []string{"missing", "pending", "failure", "stale", "forged", "success"} {
		t.Run(mode, func(t *testing.T) {
			f, h := hostedFixture(t)
			if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
				p, err := getProject(tx)
				if err != nil {
					return err
				}
				p.Publication.ReviewProviders = []domain.ExternalReviewPolicy{{Provider: "codex", ActorID: 42, AppID: 99, CheckNames: map[string]string{"code_review": "Codex code"}, Kinds: []string{"code_review"}}}
				return tx.Put("project", p.ID, p)
			}); err != nil {
				t.Fatal(err)
			}
			f.implement(t, "src/feature.txt")
			if err := f.w.Tick(context.Background()); err != nil {
				t.Fatal(err)
			}
			f.reviewAndValidate(t)
			h.ci = "success"
			h.external = func(spec domain.PullRequestSpec) []domain.ExternalReview {
				if mode == "missing" {
					return nil
				}
				report := domain.ExternalReview{Provider: "codex", Kind: "code_review", ActorID: 42, AppID: 99, CheckID: 20, CheckName: "Codex code", ReportID: 10, Commit: spec.Head, State: "success"}
				if mode == "stale" {
					report.Commit = spec.Base
				}
				if mode == "forged" {
					report.ActorID++
				}
				if mode == "pending" || mode == "failure" {
					report.State = mode
				}
				return []domain.ExternalReview{stampExternalReport(report)}
			}
			c := f.candidate(t)
			if _, err := f.s.RequestPullRequestSync(context.Background(), f.owner, "sync", c.ID, domain.VersionRequest{ExpectedVersion: c.Version}); err != nil {
				t.Fatal(err)
			}
			err := f.w.Tick(context.Background())
			if mode == "success" {
				if err != nil || h.merges != 1 || f.candidate(t).State != "landed" {
					t.Fatal(err, h.merges, f.candidate(t))
				}
			} else if h.merges != 0 || f.candidate(t).State == "landed" || h.reviewCheck == "success" {
				t.Fatal("adapter boolean bypassed exact external evidence", mode)
			}
		})
	}
}

func TestExternalReviewIssueRequiresProviderResolutionOnCurrentCommit(t *testing.T) {
	f, a := ledgerReviewFixture(t)
	api := ReviewIssues{Service: f.s}
	report := domain.ExternalReview{Provider: "codex", Kind: "security_review", ActorID: 42, AppID: 99, CheckID: 20, CheckName: "Codex security", ReportID: 10, Commit: a.Run.Base, State: "failure", Summary: "Unresolved security finding", URL: "https://github.com/owner/repo/pull/7#issuecomment-10"}
	observe := func(report domain.ExternalReview) error {
		report = stampExternalReport(report)
		return f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
			p, err := getProject(tx)
			if err != nil {
				return err
			}
			p.Publication = &domain.PublicationPolicy{Mode: "github", ReviewProviders: []domain.ExternalReviewPolicy{{Provider: "codex", ActorID: 42, AppID: 99, CheckNames: map[string]string{"security_review": "Codex security"}, Kinds: []string{"security_review"}}}}
			if err := tx.Put("project", p.ID, p); err != nil {
				return err
			}
			return api.observeExternal(tx, a.Task, *a.Candidate, domain.PullRequestStatus{ExternalReviews: []domain.ExternalReview{report}})
		})
	}
	if err := observe(report); err != nil {
		t.Fatal(err)
	}
	issue := ledgerFindings(t, f)[0]
	if issue.State != "open" || issue.Finding.Severity != "unknown" {
		t.Fatal(issue)
	}
	if _, err := api.Decide(context.Background(), f.owner, "local-waiver", issue.ID, domain.ReviewIssueDecision{ExpectedVersion: issue.Version, CandidateID: a.Candidate.ID, Commit: a.Run.Base, Disposition: "accepted_risk", Reason: "local override"}); err == nil {
		t.Fatal("local override cleared provider")
	}
	report.State = "pending"
	if err := observe(report); err != nil {
		t.Fatal(err)
	}
	if ledgerFindings(t, f)[0].State != "open" {
		t.Fatal("pending cleared external issue")
	}
	report.State = "success"
	if err := observe(report); err != nil {
		t.Fatal(err)
	}
	issue = ledgerFindings(t, f)[0]
	if issue.State != "resolved_external" || issue.ResolutionCommit != a.Run.Base {
		t.Fatal(issue)
	}
}
