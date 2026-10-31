package app

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func externalReceiptFixture(t *testing.T) (*serviceFixture, domain.Candidate) {
	t.Helper()
	f := newServiceFixture(t)
	candidate := domain.Candidate{ID: "receipt-candidate", TaskID: f.task.ID, Commit: strings.Repeat("a", 40)}
	if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
		p, err := getProject(tx)
		if err != nil {
			return err
		}
		p.Publication = &domain.PublicationPolicy{Mode: "github", ReviewProviders: []domain.ExternalReviewPolicy{{Provider: "codex", ActorID: 42, AppID: 99, Kinds: []string{"code_review", "security_review"}, CheckNames: map[string]string{"code_review": "Codex code", "security_review": "Codex security"}}}}
		return tx.Put("project", p.ID, p)
	}); err != nil {
		t.Fatal(err)
	}
	return f, candidate
}

func receiptFacts(t *testing.T, f *serviceFixture, candidate domain.Candidate, id int64, state string) domain.PullRequestStatus {
	t.Helper()
	var reports []domain.ExternalReview
	if state != "missing" {
		reports = []domain.ExternalReview{{Provider: "codex", Kind: "security_review", ActorID: 42, AppID: 99, CheckID: id, CheckName: "Codex security", ReportID: 10, Commit: candidate.Commit, State: state}}
	}
	reports = append(reports, domain.ExternalReview{Provider: "codex", Kind: "code_review", ActorID: 42, AppID: 99, CheckID: 50, CheckName: "Codex code", ReportID: 10, Commit: candidate.Commit, State: "success"})
	for i := range reports {
		reports[i] = stampExternalReport(reports[i])
	}
	return fenceReceiptFacts(t, f, candidate, reports)
}

func stampExternalReport(report domain.ExternalReview) domain.ExternalReview {
	phase := int64(1)
	if report.State != "pending" {
		phase = 2
	}
	report.CheckState, report.ReportState = report.State, report.State
	report.ReportUpdatedAt = time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC).Add(time.Duration(report.CheckID*10+phase) * time.Second)
	return report
}

func fenceReceiptFacts(t *testing.T, f *serviceFixture, candidate domain.Candidate, reports []domain.ExternalReview) domain.PullRequestStatus {
	t.Helper()
	facts := domain.PullRequestStatus{Head: candidate.Commit, ExternalReviews: reports, MergeReady: true}
	if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
		return (ReviewIssues{Service: f.s}).fenceExternal(tx, f.task, candidate, &facts)
	}); err != nil {
		t.Fatal(err)
	}
	return facts
}

func TestExternalReviewMonotonicReceiptSurvivesRestartAndDisappearance(t *testing.T) {
	for _, mode := range []string{"newer pending", "terminal changes", "missing", "older run"} {
		t.Run(mode, func(t *testing.T) {
			f, candidate := externalReceiptFixture(t)
			initial := "success"
			if mode == "newer pending" {
				initial = "pending"
			}
			receiptFacts(t, f, candidate, 20, initial)
			if err := f.s.DB.Close(); err != nil {
				t.Fatal(err)
			}
			db, err := store.Open(filepath.Join(f.s.Dir, "coordica.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			f.s = New(db, f.s.Dir)
			switch mode {
			case "terminal changes":
				receiptFacts(t, f, candidate, 20, "pending")
			case "missing":
				receiptFacts(t, f, candidate, 0, "missing")
			default:
				receiptFacts(t, f, candidate, 10, "success")
			}
			stale := receiptFacts(t, f, candidate, 10, "success")
			if stale.MergeReady || stale.ExternalReviews[0].State != "success" || stale.ExternalReviews[1].State == "success" {
				t.Fatal("receipt replay approved or poisoned another kind", stale)
			}
			if mode != "newer pending" && receiptFacts(t, f, candidate, 20, "success").MergeReady {
				t.Fatal("old terminal evidence resurrected after regression")
			}
			if !receiptFacts(t, f, candidate, 21, "success").MergeReady {
				t.Fatal("fresh App-owned check could not recover")
			}
			other := candidate
			other.Commit = strings.Repeat("b", 40)
			if !receiptFacts(t, f, other, 1, "success").MergeReady {
				t.Fatal("receipt escaped its fixed head")
			}
		})
	}
}

func TestExternalReviewReceiptAndAuditRollbackTogether(t *testing.T) {
	f, candidate := externalReceiptFixture(t)
	before := len(f.snapshot(t).Events)
	err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
		facts := domain.PullRequestStatus{ExternalReviews: []domain.ExternalReview{stampExternalReport(domain.ExternalReview{Provider: "codex", Kind: "security_review", ActorID: 42, AppID: 99, CheckID: 100, CheckName: "Codex security", ReportID: 10, Commit: candidate.Commit, State: "success"})}}
		if err := (ReviewIssues{Service: f.s}).fenceExternal(tx, f.task, candidate, &facts); err != nil {
			return err
		}
		return domain.Fail("conflict", "simulated transaction rollback")
	})
	if err == nil || len(f.snapshot(t).Events) != before {
		t.Fatal("failed transaction retained audit", err)
	}
	if !receiptFacts(t, f, candidate, 1, "success").MergeReady {
		t.Fatal("rolled-back watermark survived")
	}
}

func TestExternalReviewConcurrentReceiptsRemainMonotonicAndIdempotent(t *testing.T) {
	f, candidate := externalReceiptFixture(t)
	var workers sync.WaitGroup
	errors := make(chan error, 3)
	for _, id := range []int64{10, 20, 30} {
		workers.Add(1)
		go func(id int64) {
			defer workers.Done()
			facts := domain.PullRequestStatus{ExternalReviews: []domain.ExternalReview{stampExternalReport(domain.ExternalReview{Provider: "codex", Kind: "security_review", ActorID: 42, AppID: 99, CheckID: id, CheckName: "Codex security", ReportID: 10, Commit: candidate.Commit, State: "pending"})}}
			errors <- f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
				return (ReviewIssues{Service: f.s}).fenceExternal(tx, f.task, candidate, &facts)
			})
		}(id)
	}
	workers.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	if receiptFacts(t, f, candidate, 20, "success").MergeReady {
		t.Fatal("concurrent old receipt displaced the newest")
	}
	receiptFacts(t, f, candidate, 30, "pending")
	before := len(f.snapshot(t).Events)
	receiptFacts(t, f, candidate, 30, "pending")
	if len(f.snapshot(t).Events) != before {
		t.Fatal("duplicate receipt emitted another audit")
	}
	if !receiptFacts(t, f, candidate, 30, "success").MergeReady {
		t.Fatal("newest pending check could not complete")
	}
}

func TestExternalReviewIndependentSourceVersionsRejectMixedSnapshotReplay(t *testing.T) {
	for _, mode := range []string{"older comment", "same ID older update", "same version changed verdict", "terminal report disappears"} {
		t.Run(mode, func(t *testing.T) {
			f, candidate := externalReceiptFixture(t)
			code := stampExternalReport(domain.ExternalReview{Provider: "codex", Kind: "code_review", ActorID: 42, AppID: 99,
				CheckID: 50, CheckName: "Codex code", ReportID: 10, Commit: candidate.Commit, State: "success"})
			report := stampExternalReport(domain.ExternalReview{Provider: "codex", Kind: "security_review", ActorID: 42, AppID: 99,
				CheckID: 20, CheckName: "Codex security", ReportID: 11, Commit: candidate.Commit, State: "pending"})
			report.CheckState = "success" // A later API read already sees the App's terminal check.
			if mode == "terminal report disappears" {
				report.State, report.ReportState = "success", "success"
			}
			observe := func(r domain.ExternalReview) domain.PullRequestStatus {
				return fenceReceiptFacts(t, f, candidate, []domain.ExternalReview{code, r})
			}
			observe(report)
			stale := report
			stale.State, stale.ReportState = "success", "success"
			switch mode {
			case "older comment":
				stale.ReportID--
				stale.ReportUpdatedAt = stale.ReportUpdatedAt.Add(-time.Second)
			case "same ID older update":
				stale.ReportUpdatedAt = stale.ReportUpdatedAt.Add(-time.Second)
			case "terminal report disappears":
				missing := report
				missing.ReportID, missing.ReportUpdatedAt = 0, time.Time{}
				observe(missing)
			}
			if observe(stale).MergeReady {
				t.Fatal("old report reused an unchanged successful App check")
			}
			fresh := report
			fresh.ReportID++
			fresh.ReportUpdatedAt = fresh.ReportUpdatedAt.Add(time.Second)
			fresh.State, fresh.ReportState = "success", "success"
			if !observe(fresh).MergeReady {
				t.Fatal("new structured evidence could not recover the unchanged successful check")
			}
		})
	}
}
