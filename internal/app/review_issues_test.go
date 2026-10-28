package app

import (
	"context"
	"strings"
	"sync"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func TestReviewLedgerMissingProvenanceCannotSilentlyClearIssue(t *testing.T) {
	for _, kind := range []string{"run", "candidate"} {
		t.Run(kind, func(t *testing.T) {
			f, a := ledgerReviewFixture(t)
			if err := ledgerComplete(f, a, "negative", negativeLedgerReview(a)); err != nil {
				t.Fatal(err)
			}
			if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
				id := a.Run.ID
				if kind == "candidate" {
					id = a.Candidate.ID
				}
				return tx.Delete(kind, id)
			}); err != nil {
				t.Fatal(err)
			}
			seedLedgerCandidate(t, f, strings.Repeat("e", 40))
			before := len(f.snapshot(t).Runs)
			if next := f.claim(t, "missing-provenance"); next != nil {
				t.Fatal("missing issue proof was silently discarded", next)
			}
			if len(f.snapshot(t).Runs) != before || ledgerFindings(t, f)[0].State != "open" {
				t.Fatal("missing proof minted work or erased the issue")
			}
		})
	}
}

func TestReviewLedgerConcurrentOwnerAndReviewerCannotOverwriteEachOther(t *testing.T) {
	f, a := ledgerReviewFixture(t)
	if err := ledgerComplete(f, a, "negative", negativeLedgerReview(a)); err != nil {
		t.Fatal(err)
	}
	seedLedgerCandidate(t, f, strings.Repeat("f", 40))
	next := f.claim(t, "race-review")
	issue := ledgerFindings(t, f)[0]
	approve := testReview(*next, domain.Review{Candidate: next.Run.Base, Verdict: "approve", Summary: "0 findings", Findings: []string{}})
	in := domain.ReviewIssueDecision{ExpectedVersion: issue.Version, CandidateID: next.Candidate.ID, Commit: next.Run.Base, Disposition: "accepted_risk", Reason: "Owner's explicit bounded exception"}
	start := make(chan struct{})
	errors := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, err := (ReviewIssues{Service: f.s}).Decide(context.Background(), f.owner, "race-owner", issue.ID, in)
		errors <- err
	}()
	go func() { defer wg.Done(); <-start; errors <- ledgerComplete(f, *next, "race-review-result", *approve) }()
	close(start)
	wg.Wait()
	close(errors)
	successes := 0
	for err := range errors {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatal("version fence failed", successes)
	}
	got := ledgerFindings(t, f)[0]
	if got.Version != issue.Version+1 || (got.State != "accepted_risk" && got.State != "fixed") {
		t.Fatal("concurrent result overwrote disposition", got)
	}
}

func ledgerReviewFixture(t *testing.T) (*serviceFixture, domain.Assignment) {
	t.Helper()
	f := newServiceFixture(t)
	seedLedgerCandidate(t, f, strings.Repeat("c", 40))
	a := f.claim(t, "first-ledger-review")
	if a == nil || a.Run.ReviewContractVersion != domain.ReviewContractVersion {
		t.Fatal("structured review not negotiated", a)
	}
	return f, *a
}

func seedLedgerCandidate(t *testing.T, f *serviceFixture, commit string) {
	t.Helper()
	if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
		var task domain.Task
		if err := tx.Get("task", f.task.ID, &task); err != nil {
			return err
		}
		p, err := getProject(tx)
		if err != nil {
			return err
		}
		rules := domain.ReviewRules{Version: 1, Base: strings.Repeat("a", 40), Files: []domain.ReviewRuleFile{}, Changed: []string{}}
		rules.Digest = domain.ReviewRulesDigest(rules)
		c := domain.Candidate{ID: "candidate_" + commit[:4], Version: 1, TaskID: task.ID, State: "reviewing", Commit: commit, Main: rules.Base,
			AuthorizationVersion: task.AuthorizationVersion, PolicyVersion: p.PolicyVersion, BaselineVersion: task.BaselineVersion, BaselineDigest: task.BaselineDigest, ReviewRules: &rules}
		task.State, task.CurrentCandidateID, task.HoldReason = "awaiting_integration", c.ID, ""
		if err := tx.Put("candidate", c.ID, c); err != nil {
			return err
		}
		return tx.Put("task", task.ID, task)
	}); err != nil {
		t.Fatal(err)
	}
}

func ledgerComplete(f *serviceFixture, a domain.Assignment, key string, v domain.Review) error {
	_, err := f.s.Complete(context.Background(), f.node, key, a.Run.ID, domain.CompleteRequest{Generation: a.Run.Generation, Status: "succeeded", Quiescent: true, SessionID: "independent-review", Review: &v})
	return err
}

func ledgerFindings(t *testing.T, f *serviceFixture) []domain.ReviewIssue {
	t.Helper()
	page, err := (ReviewIssues{Service: f.s}).List(context.Background(), f.owner, domain.ReviewIssueQuery{TaskID: f.task.ID, Limit: 64})
	if err != nil {
		t.Fatal(err)
	}
	return page.Items
}

func negativeLedgerReview(a domain.Assignment) domain.Review {
	return domain.Review{Candidate: a.Run.Base, Verdict: "changes_requested", Summary: "1 actionable finding", Findings: []string{}, RulesDigest: a.Run.ReviewRulesDigest,
		Issues: []domain.ReviewFinding{{Key: "duplicate_write", Severity: "P1", Title: "Retry creates a duplicate", Path: "src/store.go", Line: 12, Trigger: "request replay", Evidence: "second write occurs before idempotency lookup", Impact: "duplicate persistent item"}}, Resolutions: []domain.ReviewResolution{}}
}

func TestReviewLedgerRequiresExplicitCurrentProofAndFencesOldResults(t *testing.T) {
	f, first := ledgerReviewFixture(t)
	if err := ledgerComplete(f, first, "negative", negativeLedgerReview(first)); err != nil {
		t.Fatal(err)
	}
	issues := ledgerFindings(t, f)
	if len(issues) != 1 || issues[0].State != "open" || issues[0].SourceRunID != first.Run.ID {
		t.Fatal(issues)
	}
	seedLedgerCandidate(t, f, strings.Repeat("d", 40))
	next := f.claim(t, "next-ledger-review")
	if next == nil || next.ReviewContext == nil || len(next.ReviewContext.Issues) != 1 {
		t.Fatal("known problem disappeared", next)
	}
	approve := domain.Review{Candidate: next.Run.Base, Verdict: "approve", Summary: "0 findings", Findings: []string{}, RulesDigest: next.Run.ReviewRulesDigest, Issues: []domain.ReviewFinding{}, Resolutions: []domain.ReviewResolution{}}
	if ledgerComplete(f, *next, "unproven-approval", approve) == nil {
		t.Fatal("omitting an old finding approved new code")
	}
	if ledgerFindings(t, f)[0].Version != issues[0].Version {
		t.Fatal("invalid result partially settled issues")
	}
	approve.Resolutions = []domain.ReviewResolution{{IssueID: issues[0].ID, ExpectedVersion: issues[0].Version, Disposition: "fixed", Reason: "Current src/store.go checks the request key before writing; replay regression passes"}}
	if err := ledgerComplete(f, *next, "verified-approval", approve); err != nil {
		t.Fatal(err)
	}
	if err := ledgerComplete(f, first, "late-old-review", negativeLedgerReview(first)); err != nil {
		t.Fatal(err)
	}
	got := ledgerFindings(t, f)[0]
	if got.ID != issues[0].ID || got.State != "fixed" || got.ResolutionCommit != next.Run.Base {
		t.Fatal("late result rewrote current issue proof", got)
	}
	if err := ledgerComplete(f, *next, "verified-approval", approve); err != nil {
		t.Fatal(err)
	}
	if ledgerFindings(t, f)[0].Version != got.Version {
		t.Fatal("duplicate receipt created another issue revision")
	}
}

func TestReviewLedgerOwnerRiskIsVersionedNotAClaimedFixOrReviewApproval(t *testing.T) {
	f, a := ledgerReviewFixture(t)
	if err := ledgerComplete(f, a, "negative", negativeLedgerReview(a)); err != nil {
		t.Fatal(err)
	}
	seedLedgerCandidate(t, f, strings.Repeat("e", 40))
	next := f.claim(t, "owner-race")
	issue := ledgerFindings(t, f)[0]
	in := domain.ReviewIssueDecision{ExpectedVersion: issue.Version, CandidateID: next.Candidate.ID, Commit: next.Run.Base, Disposition: "accepted_risk", Reason: "Owner accepts this bounded private-host risk"}
	api := ReviewIssues{Service: f.s}
	if _, err := api.Decide(context.Background(), f.node, "node-risk", issue.ID, in); err == nil {
		t.Fatal("node accepted risk")
	}
	wrong := in
	wrong.Commit = a.Run.Base
	if _, err := api.Decide(context.Background(), f.owner, "wrong-risk", issue.ID, wrong); err == nil {
		t.Fatal("old commit disposition carried forward")
	}
	if _, err := api.Decide(context.Background(), f.owner, "owner-risk", issue.ID, in); err != nil {
		t.Fatal(err)
	}
	got := ledgerFindings(t, f)[0]
	if got.State != "accepted_risk" || got.State == "fixed" || got.Version != issue.Version+1 {
		t.Fatal(got)
	}
	approve := testReview(*next, domain.Review{Candidate: next.Run.Base, Verdict: "approve", Summary: "0 actionable", Findings: []string{}})
	if ledgerComplete(f, *next, "stale-resolution", *approve) == nil {
		t.Fatal("stale issue version overrode concurrent Owner action")
	}
	if ledgerFindings(t, f)[0].State != "accepted_risk" {
		t.Fatal("rejected result erased Owner decision")
	}
}

func TestReviewLedgerRejectsCrossTaskAndExternalResolutionAtomically(t *testing.T) {
	for _, source := range []string{"another task", "external/codex/security_review/2/3"} {
		t.Run(source, func(t *testing.T) {
			f, a := ledgerReviewFixture(t)
			if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
				issue := domain.ReviewIssue{ID: "issue_foreign", Version: 1, TaskID: f.task.ID, Source: source, State: "open"}
				if source == "another task" {
					issue.TaskID = "other"
					issue.Source = "internal"
				}
				return tx.Put("review_issue", issue.ID, issue)
			}); err != nil {
				t.Fatal(err)
			}
			v := negativeLedgerReview(a)
			v.Resolutions = []domain.ReviewResolution{{IssueID: "issue_foreign", ExpectedVersion: 1, Disposition: "false_positive", Reason: "not mine"}}
			if ledgerComplete(f, a, "invalid-resolution", v) == nil {
				t.Fatal("resolved foreign issue")
			}
			got := ledgerFindings(t, f)
			for _, issue := range got {
				if issue.Source == "internal" && issue.ID != "issue_foreign" {
					t.Fatal("partial finding was committed", issue)
				}
			}
		})
	}
}
