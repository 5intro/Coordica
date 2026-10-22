package app

import (
	"bytes"
	"context"
	"coordica/internal/domain"
	"coordica/internal/store"
	"testing"
)

func TestPlanningAnswerFencesSameGoalNewResultAndAtomicReplay(t *testing.T) {
	f, goal, result := planningFixture(t, false)
	goal.LeaderRunID = "accepted-leader-run"
	result.Clarifications = []string{"要使用哪种存储？"}
	goal.LeaderResult = &result
	ctx := context.Background()
	if err := f.s.DB.Update(ctx, func(tx *store.Tx) error { return tx.Put("planning_document", "current", goal) }); err != nil {
		t.Fatal(err)
	}
	req := domain.PlanningMessageRequest{ExpectedVersion: goal.Version, ReplyToRunID: goal.LeaderRunID, Question: result.Clarifications[0], Text: "SQLite"}
	// The center remains paused; accepting an answer can queue evaluation only.
	project, _ := f.s.Project(ctx)
	if _, err := f.s.ProjectAction(ctx, f.owner, "pause", "pause", domain.VersionRequest{ExpectedVersion: project.Version}); err != nil {
		t.Fatal(err)
	}
	before := f.snapshot(t)
	for _, bad := range []domain.PlanningMessageRequest{
		{ExpectedVersion: goal.Version, ReplyToRunID: "older-run", Question: req.Question, Text: req.Text},
		{ExpectedVersion: goal.Version, ReplyToRunID: req.ReplyToRunID, Question: "another question", Text: req.Text},
		{ExpectedVersion: goal.Version + 1, ReplyToRunID: req.ReplyToRunID, Question: req.Question, Text: req.Text},
	} {
		if _, err := f.s.SchedulePlanning(ctx, f.owner, ID("bad"), bad); err == nil {
			t.Fatal("stale question accepted")
		}
	}
	after := f.snapshot(t)
	if len(after.LeaderMessages) != len(before.LeaderMessages) || len(after.PlanningRuns) != len(before.PlanningRuns) {
		t.Fatal("rejection partially mutated state")
	}
	raw, err := f.s.SchedulePlanning(ctx, f.owner, "answer-key", req)
	receipt := decodeTest[domain.PlanningAnswerReceipt](t, raw, err)
	replay, err := f.s.SchedulePlanning(ctx, f.owner, "answer-key", req)
	if err != nil || !bytes.Equal(raw, replay) {
		t.Fatal("answer replay duplicated or changed", err)
	}
	if receipt.ReplyToRunID != req.ReplyToRunID || receipt.Message.Text != planningAnswerText(req) || receipt.Run.State != "queued" {
		t.Fatal(receipt)
	}
	after = f.snapshot(t)
	if !after.Project.Paused || after.Automation.Enabled || after.PlanningDocument.AuthorizationVersion != before.PlanningDocument.AuthorizationVersion {
		t.Fatal("reply resumed/authorized work")
	}
	req.Text = "different"
	if _, err := f.s.SchedulePlanning(ctx, f.owner, "answer-key", req); err == nil {
		t.Fatal("key reused with new prose")
	}
}
