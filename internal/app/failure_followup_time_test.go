package app

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func TestFailureFollowupUsesOptionalTimePolicyAndIndependentCounters(t *testing.T) {
	for _, verification := range []bool{false, true} {
		kind := "development"
		if verification {
			kind = "verification"
		}
		for _, scenario := range []struct {
			name         string
			legacySpent  bool
			enforce      bool
			counterSpent bool
			resume       bool
		}{
			{name: "zero allowance", resume: true},
			{name: "spent legacy allowance", legacySpent: true, resume: true},
			{name: "explicitly enforced allowance", legacySpent: true, enforce: true},
			{name: "zero optional allowance under enforcement", enforce: true, resume: true},
			{name: "repair or retry exhausted", counterSpent: true, resume: true},
		} {
			t.Run(kind+"/"+scenario.name, func(t *testing.T) {
				f, source := failureFixture(t)
				setTestTimeLimits(t, f.s, false)
				source = followupTimeSource(t, f, source, verification, scenario.legacySpent, scenario.counterSpent)
				assessment := runRecovery(t, f, followupPlan(source))
				if assessment.State != "waiting_followup" {
					t.Fatal("follow-up was not admitted", assessment)
				}
				landed := completeTimeFollowup(t, f, assessment)
				// A user may enable the optional allowance while a child is working.
				setTestTimeLimits(t, f.s, scenario.enforce)
				before := requirementTask(t, f, source.SourceTask.ID)
				if err := (FailureRecoveries{Service: f.s}).queue(context.Background()); err != nil {
					t.Fatal(err)
				}
				after := requirementTask(t, f, source.SourceTask.ID)
				got := failureCases(t, f.s)[0]
				if !scenario.resume {
					if got.State != "needs_owner" || after.HoldReason != "human_decision" ||
						after.BudgetSeconds != before.BudgetSeconds || after.UsedSeconds != before.UsedSeconds ||
						after.ReservedSeconds != before.ReservedSeconds || after.Repairs != before.Repairs ||
						after.Retries != before.Retries || after.AuthorizationVersion != before.AuthorizationVersion {
						t.Fatal("enforced allowance or independent counters were bypassed", got, after)
					}
					return
				}
				if got.State != "applied" || after.State != "ready" || after.HoldReason != "" ||
					after.BudgetSeconds != before.BudgetSeconds || after.UsedSeconds != before.UsedSeconds ||
					after.ReservedSeconds != before.ReservedSeconds || after.AuthorizationVersion != before.AuthorizationVersion+1 {
					t.Fatal("disabled allowance blocked recovery or accounting/authority changed", got, after)
				}
				if verification {
					if after.Retries != before.Retries || after.Repairs != before.Repairs || after.Verification.Commit != landed {
						t.Fatal("verification lost its counter or fixed candidate", after)
					}
				} else if after.Repairs != before.Repairs || after.Retries != before.Retries {
					t.Fatal("development repair counter changed", after)
				}
				if err := (FailureRecoveries{Service: f.s}).queue(context.Background()); err != nil {
					t.Fatal(err)
				}
				if repeated := requirementTask(t, f, after.ID); !reflect.DeepEqual(after, repeated) {
					t.Fatal("duplicate follow-up settlement changed the source", repeated)
				}
			})
		}
	}
}

// The source is already stopped. Seed the optional policy and cumulative facts
// before diagnosis, preserving its frozen failure/authority tuple.
func followupTimeSource(t *testing.T, f *serviceFixture, source domain.FailureAssessment, verification, spent, counterSpent bool) domain.FailureAssessment {
	t.Helper()
	source.SourceTask.BudgetSeconds, source.SourceTask.MaxRunSeconds = 0, 0
	if spent {
		source.SourceTask.BudgetSeconds = source.SourceTask.UsedSeconds
		source.SourceTask.MaxRunSeconds = source.SourceTask.BudgetSeconds
	}
	if counterSpent {
		if verification {
			source.SourceTask.Retries = source.SourceTask.MaxRetries
		} else {
			source.SourceTask.Repairs = source.SourceTask.MaxRepairs
		}
	}
	if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
		if verification {
			suite := domain.TestSuite{ID: "smoke", Version: 1, Name: "Smoke", Enabled: true, Stage: "project",
				Checks: []domain.Check{{Name: "probe", Command: "probe", TimeoutSeconds: 3}}}
			source.SourceTask.Verification = &domain.VerificationSpec{Suite: suite, Commit: strings.Repeat("a", 40), Digest: suiteDigest(suite)}
			source.SourceTask.HoldReason = "tests_failed"
			var run domain.Run
			if err := tx.Get("run", source.SourceRunID, &run); err != nil {
				return err
			}
			run.Kind, run.State, run.Base = "verification", "failed", source.SourceTask.Verification.Commit
			run.TestReport = &domain.TestReport{Commit: run.Base, SuiteID: suite.ID, SuiteVersion: suite.Version, Digest: suiteDigest(suite)}
			if err := tx.Put("run", run.ID, run); err != nil {
				return err
			}
			if err := tx.Put("test_suite", suite.ID, suite); err != nil {
				return err
			}
		}
		if err := tx.Put("task", source.SourceTask.ID, source.SourceTask); err != nil {
			return err
		}
		return tx.Put("failure_assessment", source.ID, source)
	}); err != nil {
		t.Fatal(err)
	}
	return source
}

func completeTimeFollowup(t *testing.T, f *serviceFixture, assessment domain.FailureAssessment) string {
	t.Helper()
	landed := strings.Repeat("e", 40)
	if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
		var child domain.Task
		if err := tx.Get("task", assessment.FollowupTaskID, &child); err != nil {
			return err
		}
		// Integration is injected here; real candidate/check/review/landing gates
		// are independently exercised by the complete integration suite.
		child.State, child.CurrentCandidateID = "integrated", "followup-landed"
		child.Version++
		if err := tx.Put("task", child.ID, child); err != nil {
			return err
		}
		return tx.Put("candidate", child.CurrentCandidateID, domain.Candidate{ID: child.CurrentCandidateID, TaskID: child.ID, State: "landed", Commit: landed,
			AuthorizationVersion: child.AuthorizationVersion, PolicyVersion: assessment.PolicyVersion,
			BaselineVersion: child.BaselineVersion, BaselineDigest: child.BaselineDigest})
	}); err != nil {
		t.Fatal(err)
	}
	return landed
}
