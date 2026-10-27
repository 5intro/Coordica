package app

import (
	"reflect"
	"strings"
	"unicode/utf8"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func boundedFailureText(v string, limit int) (string, bool) {
	v = strings.ToValidUTF8(v, "�")
	if len(v) <= limit {
		return v, false
	}
	end := limit
	for end > 0 && !utf8.RuneStart(v[end]) {
		end--
	}
	return v[:end], true
}

func failureCurrent(tx *store.Tx, f domain.FailureAssessment, waiting bool) bool {
	var task domain.Task
	if tx.Get("task", f.SourceTask.ID, &task) != nil || task.CurrentRunID != "" ||
		domain.TaskFinished(task) || requireQuiescence(tx, task.ID) != nil || goalAuthority(tx, &task, false) != nil {
		return false
	}
	project, err := getProject(tx)
	if err != nil || project.PolicyVersion != f.PolicyVersion || project.TestPolicyVersion != f.TestPolicyVersion {
		return false
	}
	expected := f.SourceTask
	if waiting {
		expected.Version = f.WaitingTaskVersion
		expected.HoldReason = "failure_followup"
	}
	if !reflect.DeepEqual(task, expected) {
		return false
	}
	var run domain.Run
	if tx.Get("run", f.SourceRunID, &run) != nil || run.Version != f.SourceRunVersion || !run.Quiescent {
		return false
	}
	if f.SourceCandidateID != "" {
		var c domain.Candidate
		if tx.Get("candidate", f.SourceCandidateID, &c) != nil || c.Version != f.SourceCandidateVersion ||
			c.State != "rejected" || c.Commit != f.SourceCommit || c.TaskID != task.ID {
			return false
		}
	}
	return true
}

func failurePlanningCurrent(tx *store.Tx, run domain.PlanningRun) bool {
	if run.FailureAssessmentID == "" {
		return true
	}
	settings, err := automation(tx)
	if err != nil || !settings.Enabled || settings.Version != run.AutomationVersion {
		return false
	}
	var f domain.FailureAssessment
	return run.Purpose == domain.FailureRecoveryPurpose &&
		tx.Get("failure_assessment", run.FailureAssessmentID, &f) == nil &&
		f.State == "assessing" && f.RunID == run.ID && failureCurrent(tx, f, false)
}

func blockFailureAssessment(tx *store.Tx, s *Service, run domain.PlanningRun, detail string) error {
	if run.FailureAssessmentID == "" {
		return nil
	}
	var f domain.FailureAssessment
	if err := tx.Get("failure_assessment", run.FailureAssessmentID, &f); err != nil {
		return err
	}
	if f.RunID != run.ID || f.State != "assessing" {
		return nil
	}
	f.State = "needs_owner"
	if !failureCurrent(tx, f, false) {
		f.State = "stale"
	}
	f.Detail, _ = boundedFailureText(detail, 4096)
	f.Version++
	f.UpdatedAt = s.Now()
	return tx.Put("failure_assessment", f.ID, f)
}
