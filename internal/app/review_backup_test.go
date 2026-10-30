package app

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func TestReviewBackupRestoreKeepsIssuesRulesAndImmutableReceipts(t *testing.T) {
	s, w, _, _ := bkFixture(t)
	ctx := context.Background()
	owner := Actor{ID: "owner", Role: "owner"}
	var candidate domain.Candidate
	if err := s.DB.View(ctx, func(tx *store.Tx) error { return tx.Get("candidate", "pending_candidate", &candidate) }); err != nil {
		t.Fatal(err)
	}
	rules := domain.ReviewRules{Version: 1, Base: candidate.Main, Files: []domain.ReviewRuleFile{{Path: "REVIEW.md", Text: "Verify idempotency\r\n"}}, Changed: []string{}}
	rules.Digest = domain.ReviewRulesDigest(rules)
	candidate.ReviewRules = &rules
	issue := domain.ReviewIssue{ID: "issue_backup", TaskID: candidate.TaskID, Version: 2, Source: "internal", State: "accepted_risk", ResolutionCommit: candidate.Commit, ResolutionReason: "bounded trusted-host risk"}
	if err := s.DB.Update(ctx, func(tx *store.Tx) error {
		if err := tx.Put("candidate", candidate.ID, candidate); err != nil {
			return err
		}
		if err := tx.Put("review_issue", issue.ID, issue); err != nil {
			return err
		}
		if err := tx.Put("review_issue_history", issue.ID+"_v2", issue); err != nil {
			return err
		}
		return saveReviewReceipt(tx, domain.Run{ID: "run_review_backup", CandidateID: candidate.ID}, domain.Review{RunID: "run_review_backup", Candidate: candidate.Commit, Verdict: "changes_requested", RulesDigest: rules.Digest, Findings: []string{}, Issues: []domain.ReviewFinding{}, Resolutions: []domain.ReviewResolution{}})
	}); err != nil {
		t.Fatal(err)
	}
	backup, err := w.Backup(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "restore")
	if err = RestoreBackup(ctx, backup, dest); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(dest, "coordica.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.View(ctx, func(tx *store.Tx) error {
		var got, historical domain.ReviewIssue
		var c domain.Candidate
		var receipt domain.Review
		var sourceCandidateID string
		if err := tx.Get("review_issue", issue.ID, &got); err != nil {
			return err
		}
		if err := tx.Get("review_issue_history", issue.ID+"_v2", &historical); err != nil {
			return err
		}
		if err := tx.Get("review_receipt", "run_review_backup", &receipt); err != nil {
			return err
		}
		if err := tx.Get("review_source_candidate", "run_review_backup", &sourceCandidateID); err != nil {
			return err
		}
		if err := tx.Get("candidate", candidate.ID, &c); err != nil {
			return err
		}
		if !reflect.DeepEqual(got, issue) || !reflect.DeepEqual(historical, issue) || !reflect.DeepEqual(c.ReviewRules, &rules) || receipt.Verdict != "changes_requested" || receipt.RulesDigest != rules.Digest || sourceCandidateID != candidate.ID {
			t.Fatal("review provenance lost during restore", got, c, receipt)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
