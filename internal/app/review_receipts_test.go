package app

import (
	"context"
	"strings"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func TestReviewReceiptProvenanceSurvivesMutableCandidateChanges(t *testing.T) {
	for _, mode := range []string{"rebuilt", "removed candidate", "older receipt", "wrong commit", "wrong run", "wrong rules", "wrong issue", "wrong source"} {
		t.Run(mode, func(t *testing.T) {
			f, a := ledgerReviewFixture(t)
			if err := ledgerComplete(f, a, "negative", negativeLedgerReview(a)); err != nil {
				t.Fatal(err)
			}
			if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
				var c domain.Candidate
				if err := tx.Get("candidate", a.Candidate.ID, &c); err != nil {
					return err
				}
				c.Commit, c.Main, c.Review, c.ReviewRules = strings.Repeat("f", 40), strings.Repeat("b", 40), nil, nil
				if err := tx.Put("candidate", c.ID, c); err != nil {
					return err
				}
				if mode == "removed candidate" {
					return tx.Delete("candidate", c.ID)
				}
				if mode == "older receipt" {
					return tx.Delete("review_source_candidate", a.Run.ID)
				}
				if mode == "wrong source" {
					return tx.Put("review_source_candidate", a.Run.ID, "another_candidate")
				}
				var receipt domain.Review
				if err := tx.Get("review_receipt", a.Run.ID, &receipt); err != nil {
					return err
				}
				switch mode {
				case "wrong commit":
					receipt.Candidate = c.Commit
				case "wrong run":
					receipt.RunID = "another_run"
				case "wrong rules":
					receipt.RulesDigest = "another_rules_digest"
				case "wrong issue":
					receipt.Issues[0].Key = "another_issue"
				}
				return tx.Put("review_receipt", a.Run.ID, receipt)
			}); err != nil {
				t.Fatal(err)
			}
			if err := f.s.DB.View(context.Background(), func(tx *store.Tx) error {
				var task domain.Task
				if err := tx.Get("task", a.Task.ID, &task); err != nil {
					return err
				}
				got, err := applicableReviewIssues(tx, task, a.Run.PolicyVersion)
				valid := mode == "rebuilt" || mode == "removed candidate" || mode == "older receipt"
				if valid && (err != nil || len(got) != 1 || got[0].SourceRunID != a.Run.ID) {
					t.Fatal("mutable candidate invalidated immutable proof", got, err)
				}
				if !valid && (err == nil || len(got) != 0) {
					t.Fatal("corrupt provenance silently skipped or trusted", got, err)
				}
				if reviewLedgerReady(tx, a.Task.ID, strings.Repeat("f", 40)) == nil {
					t.Fatal("new commit bypassed issue gate")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestReviewReceiptCannotBeReplacedOrRebound(t *testing.T) {
	f, a := ledgerReviewFixture(t)
	negative := negativeLedgerReview(a)
	if err := ledgerComplete(f, a, "negative", negative); err != nil {
		t.Fatal(err)
	}
	negative.RunID = a.Run.ID
	for _, rebind := range []bool{false, true} {
		if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
			r, v := a.Run, negative
			if rebind {
				r.CandidateID = "another_candidate"
			} else {
				v.Summary = "replacement verdict"
			}
			if err := saveReviewReceipt(tx, r, v); err == nil {
				t.Fatal("immutable source overwritten")
			}
			var got domain.Review
			var candidateID string
			if err := tx.Get("review_receipt", r.ID, &got); err != nil {
				return err
			}
			if err := tx.Get("review_source_candidate", r.ID, &candidateID); err != nil {
				return err
			}
			if got.Summary != negative.Summary || candidateID != a.Run.CandidateID {
				t.Fatal("failed write changed provenance", got, candidateID)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReviewReceiptOlderResolutionUsesPinnedRulesInsteadOfOldIssueMetadata(t *testing.T) {
	f, first := ledgerReviewFixture(t)
	if err := ledgerComplete(f, first, "negative", negativeLedgerReview(first)); err != nil {
		t.Fatal(err)
	}
	seedLedgerCandidate(t, f, strings.Repeat("d", 40))
	if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
		var c domain.Candidate
		if err := tx.Get("candidate", "candidate_dddd", &c); err != nil {
			return err
		}
		c.Main, c.ReviewRules.Base = strings.Repeat("b", 40), strings.Repeat("b", 40)
		c.ReviewRules.Digest = domain.ReviewRulesDigest(*c.ReviewRules)
		return tx.Put("candidate", c.ID, c)
	}); err != nil {
		t.Fatal(err)
	}
	next := f.claim(t, "new-rules-review")
	v := testReview(*next, domain.Review{Candidate: next.Run.Base, Verdict: "approve", Summary: "0 remaining issues", Findings: []string{}})
	if err := ledgerComplete(f, *next, "resolved", *v); err != nil {
		t.Fatal(err)
	}
	issue := ledgerFindings(t, f)[0]
	if issue.RulesDigest != next.Run.ReviewRulesDigest {
		t.Fatal("new resolution did not pin current rules")
	}
	// The original PR stored the correct receipt, but left this metadata at the
	// first finding's digest and did not yet record source candidate identity.
	issue.RulesDigest = first.Run.ReviewRulesDigest
	if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
		if err := tx.Put("review_issue", issue.ID, issue); err != nil {
			return err
		}
		return tx.Delete("review_source_candidate", next.Run.ID)
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.s.DB.View(context.Background(), func(tx *store.Tx) error {
		if err := reviewLedgerReady(tx, issue.TaskID, next.Run.Base); err != nil {
			t.Fatal("older valid receipt rejected", err)
		}
		if err := reviewLedgerReady(tx, issue.TaskID, strings.Repeat("e", 40)); err == nil {
			t.Fatal("older disposition carried into a new commit")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
