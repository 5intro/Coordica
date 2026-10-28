package app

import (
	"context"
	"strings"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func criticalRecovery(t *testing.T) (*serviceFixture, domain.FailureAssessment) {
	t.Helper()
	f, source := failureFixture(t)
	source = followupTimeSource(t, f, source, true, false, true)
	if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
		suite := source.SourceTask.Verification.Suite
		suite.BlockMergesOnFailure = true
		source.SourceTask.Verification.Suite = suite
		source.SourceTask.Verification.Digest = suiteDigest(suite)
		var r domain.Run
		if err := tx.Get("run", source.SourceRunID, &r); err != nil {
			return err
		}
		r.TestReport.Digest = suiteDigest(suite)
		if err := tx.Put("run", r.ID, r); err != nil {
			return err
		}
		if err := tx.Put("task", source.SourceTask.ID, source.SourceTask); err != nil {
			return err
		}
		if err := tx.Put("test_suite", suite.ID, suite); err != nil {
			return err
		}
		return tx.Put("failure_assessment", source.ID, source)
	}); err != nil {
		t.Fatal(err)
	}
	got := runRecovery(t, f, followupPlan(source))
	if got.State != "waiting_followup" {
		t.Fatal(got)
	}
	if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
		var child domain.Task
		if err := tx.Get("task", got.FollowupTaskID, &child); err != nil {
			return err
		}
		child.State, child.CurrentCandidateID = "awaiting_integration", "repair-fixed"
		c := domain.Candidate{ID: child.CurrentCandidateID, TaskID: child.ID, State: "ready_to_land", Commit: strings.Repeat("c", 40),
			AuthorizationVersion: child.AuthorizationVersion, PolicyVersion: source.PolicyVersion,
			BaselineVersion: child.BaselineVersion, BaselineDigest: child.BaselineDigest}
		if err := tx.Put("candidate", c.ID, c); err != nil {
			return err
		}
		return tx.Put("task", child.ID, child)
	}); err != nil {
		t.Fatal(err)
	}
	return f, got
}

func TestFailureCriticalPrecheckIsBoundedAndFailsClosed(t *testing.T) {
	for _, mode := range []string{"pending", "failed", "cancelled", "candidate changed"} {
		t.Run(mode, func(t *testing.T) {
			f, got := criticalRecovery(t)
			if err := (FailureRecoveries{Service: f.s}).queue(context.Background()); err != nil {
				t.Fatal(err)
			}
			got = failureCases(t, f.s)[0]
			if got.PrecheckTaskID == "" {
				t.Fatal("critical repair would wait forever on its own merge fence")
			}
			if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
				if mode == "candidate changed" {
					var c domain.Candidate
					if err := tx.Get("candidate", "repair-fixed", &c); err != nil {
						return err
					}
					c.Commit = strings.Repeat("d", 40)
					return tx.Put("candidate", c.ID, c)
				}
				var check domain.Task
				if err := tx.Get("task", got.PrecheckTaskID, &check); err != nil {
					return err
				}
				if mode == "failed" {
					check.HoldReason = "tests_failed"
				}
				if mode == "cancelled" {
					check.State = "cancelled"
				}
				return tx.Put("task", check.ID, check)
			}); err != nil {
				t.Fatal(err)
			}
			if err := (FailureRecoveries{Service: f.s}).queue(context.Background()); err != nil {
				t.Fatal(err)
			}
			latest := failureCases(t, f.s)[0]
			if latest.PrecheckTaskID != got.PrecheckTaskID || len(f.snapshot(t).Tasks) != 4 {
				t.Fatal("duplicate candidate checks minted", latest)
			}
			if mode == "pending" {
				if latest.State != "waiting_followup" {
					t.Fatal("pending check was treated as a failure")
				}
			} else if latest.State != "needs_owner" || requirementTask(t, f, got.SourceTask.ID).FailureRecheckID != "" {
				t.Fatal("failed/stale check authorized source recheck", latest)
			}
		})
	}
}
