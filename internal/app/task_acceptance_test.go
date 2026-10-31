package app

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/gitops"
	"coordica/internal/store"
)

func blockedAcceptanceFixture(t *testing.T) (*itFixture, domain.Task, domain.Candidate, domain.Assignment) {
	t.Helper()
	f := newITFixture(t)
	f.implement(t, "src/feature.txt")
	c := f.candidate(t)
	review := f.claim(t, "review")
	_, err := f.s.Complete(context.Background(), f.node, "blocked-review", review.Run.ID, domain.CompleteRequest{
		Generation: review.Run.Generation, Status: "succeeded", Quiescent: true,
		Review: testReview(review, domain.Review{Candidate: c.Commit, Verdict: "blocked", Summary: "Module and final acceptance conflict", Findings: []string{"Fix the module defect before handoff"}}),
	})
	if err != nil {
		t.Fatal(err)
	}
	s := f.snapshot(t)
	return f, s.Tasks[0], s.Candidates[0], review
}

func acceptanceRevisionRequest(task domain.Task, c domain.Candidate) domain.TaskAcceptanceRequest {
	return domain.TaskAcceptanceRequest{ExpectedVersion: task.Version, ExpectedCandidateID: c.ID,
		Acceptance: "Validate the module and fix its defect; final delivery remains the integration task's responsibility",
		Reason:     "Owner corrects a derived task boundary, preserving the original project acceptance"}
}

func TestTaskAcceptanceRevisionPreservesInputButInvalidatesApproval(t *testing.T) {
	f, before, old, oldReview := blockedAcceptanceFixture(t)
	ctx := context.Background()
	in := acceptanceRevisionRequest(before, old)
	raw, err := ReviseTaskAcceptance(ctx, f.s, f.owner, "revise", before.ID, in)
	after := itDecode[domain.Task](t, raw, err)
	expected := before
	expected.Acceptance, expected.AuthorizationVersion, expected.Version = in.Acceptance, before.AuthorizationVersion+1, before.Version+1
	expected.Repairs, expected.CurrentCandidateID, expected.State, expected.HoldReason = before.Repairs+1, "", "ready", "paused"
	if !reflect.DeepEqual(expected, after) || !reflect.DeepEqual(old, f.snapshot(t).Candidates[0]) {
		t.Fatal("revision reset spending, changed scope/goal or rewrote old review", after)
	}
	replay, err := ReviseTaskAcceptance(ctx, f.s, f.owner, "revise", before.ID, in)
	if err != nil || string(replay) != string(raw) {
		t.Fatal("revision replay changed identity or consumed another repair", err)
	}
	if _, err = ReviseTaskAcceptance(ctx, f.s, f.owner, "stale", before.ID, in); err == nil {
		t.Fatal("old contract version was accepted")
	}
	if err = f.w.Tick(ctx); err != nil || f.snapshot(t).Candidates[0].State != "rejected" {
		t.Fatal("acceptance edit promoted old code", err)
	}
	_, err = f.s.TaskAction(ctx, f.owner, "resume-revision", after.ID, "resume", domain.VersionRequest{ExpectedVersion: after.Version})
	if err != nil {
		t.Fatal(err)
	}
	repair := f.claim(t, "implement")
	if repair.Run.Base != old.Commit || repair.Run.AuthorizationVersion != after.AuthorizationVersion ||
		repair.Task.Acceptance != in.Acceptance || repair.Candidate == nil || repair.Candidate.ID != old.ID {
		t.Fatal("new Run did not pin the Owner-authorized fixed repair input", repair)
	}
	_, err = f.s.Complete(ctx, f.node, "late-old-approve", oldReview.Run.ID, domain.CompleteRequest{
		Generation: oldReview.Run.Generation, Status: "succeeded", Quiescent: true,
		Review: &domain.Review{Candidate: old.Commit, Verdict: "approve", Summary: "late old approval", Findings: []string{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := f.snapshot(t)
	if s.Tasks[0].CurrentRunID != repair.Run.ID || s.Candidates[0].Review.Verdict != "blocked" {
		t.Fatal("late old review affected the revised execution", s)
	}
	finishAcceptanceRepair(t, f, repair, old)
}

func finishAcceptanceRepair(t *testing.T, f *itFixture, repair domain.Assignment, old domain.Candidate) {
	t.Helper()
	ctx := context.Background()
	input, err := f.w.InputBundle(ctx, repair.Run)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "new-repair")
	if err = f.g.CloneSnapshot(ctx, input, gitops.OID(repair.Run.Base), dir); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "src/feature.txt")
	if b, err := os.ReadFile(file); err != nil || string(b) != "implemented\n" {
		t.Fatal("fixed old implementation was lost", err)
	}
	if err = os.WriteFile(file, []byte("actual repaired behavior\n"), 0600); err != nil {
		t.Fatal(err)
	}
	head, err := f.g.CommitAll(ctx, dir, "repair against revised contract", gitops.Identity{})
	if err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(t.TempDir(), "repair.bundle")
	if _, err = f.g.CreateBundle(ctx, dir, gitops.OID(repair.Run.Base), head, bundle); err != nil {
		t.Fatal(err)
	}
	material, err := os.Open(bundle)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := f.s.UploadArtifact(ctx, f.node, repair.Run.ID, material)
	material.Close()
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.s.Complete(ctx, f.node, "new-repair-result", repair.Run.ID, domain.CompleteRequest{
		Generation: repair.Run.Generation, Status: "succeeded", Head: head.String(), ArtifactID: artifact.ID, Quiescent: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err = f.w.Tick(ctx); err != nil {
			t.Fatal(err)
		}
	}
	review := f.claim(t, "review")
	if review.Run.Base == old.Commit || review.Candidate.Review != nil || review.Candidate.SourceRunID != repair.Run.ID ||
		review.Run.AuthorizationVersion != repair.Run.AuthorizationVersion || review.Run.ID == old.Review.RunID {
		t.Fatal("new candidate skipped fresh independent review", review)
	}
}

func TestTaskAcceptanceRevisionRejectsUnsafeOrStaleSources(t *testing.T) {
	for _, scenario := range []string{"node", "chat", "running", "reserved", "repair limit", "wrong candidate", "source not stopped", "source lost", "policy", "goal binding", "invalid text"} {
		t.Run(scenario, func(t *testing.T) {
			f, task, candidate, _ := blockedAcceptanceFixture(t)
			actor := f.owner
			in := acceptanceRevisionRequest(task, candidate)
			switch scenario {
			case "node":
				actor = f.node
			case "chat":
				actor = Actor{ID: "chat", Role: "chat"}
			case "wrong candidate":
				in.ExpectedCandidateID = "another_candidate"
			case "invalid text":
				in.Acceptance = strings.Repeat("x", (16<<10)+1)
			}
			if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
				var source domain.Run
				if err := tx.Get("run", candidate.SourceRunID, &source); err != nil {
					return err
				}
				switch scenario {
				case "running":
					task.CurrentRunID = source.ID
				case "reserved":
					task.ReservedSeconds = 1
				case "repair limit":
					task.Repairs = task.MaxRepairs
				case "source not stopped":
					source.Quiescent = false
				case "source lost":
					source.State = "lost"
				case "policy":
					candidate.PolicyVersion++
				case "goal binding":
					source.GoalAuthorizationVersion++
				}
				if err := tx.Put("task", task.ID, task); err != nil {
					return err
				}
				if err := tx.Put("candidate", candidate.ID, candidate); err != nil {
					return err
				}
				return tx.Put("run", source.ID, source)
			}); err != nil {
				t.Fatal(err)
			}
			before := f.snapshot(t)
			if _, err := ReviseTaskAcceptance(context.Background(), f.s, actor, "unsafe", task.ID, in); err == nil {
				t.Fatal("unsafe acceptance mutation was accepted")
			}
			after := f.snapshot(t)
			if !reflect.DeepEqual(before.Tasks, after.Tasks) || !reflect.DeepEqual(before.Candidates, after.Candidates) ||
				!reflect.DeepEqual(before.Runs, after.Runs) || len(before.Events) != len(after.Events) {
				t.Fatal("rejected revision partially changed authority, evidence or audit")
			}
		})
	}
}

func TestTaskAcceptanceRepairInputCannotFollowChangedMaterialOrScope(t *testing.T) {
	for _, scenario := range []string{"changed material", "later scope"} {
		t.Run(scenario, func(t *testing.T) {
			f, before, old, _ := blockedAcceptanceFixture(t)
			ctx := context.Background()
			raw, err := ReviseTaskAcceptance(ctx, f.s, f.owner, "revise", before.ID, acceptanceRevisionRequest(before, old))
			task := itDecode[domain.Task](t, raw, err)
			if scenario == "later scope" {
				_, err = f.s.Scope(ctx, f.owner, "new-scope", task.ID, domain.ScopeRequest{
					ExpectedVersion: task.Version, AllowedPaths: []string{"src/other/**"}, Reason: "Explicit later authority change",
				})
				if err != nil {
					t.Fatal(err)
				}
				a := f.claim(t, "implement")
				if a.Run.Base == old.Commit || a.Candidate != nil || a.Run.AuthorizationVersion != task.AuthorizationVersion+1 {
					t.Fatal("later scope silently inherited old input", a)
				}
				return
			}
			if err = f.s.DB.Update(ctx, func(tx *store.Tx) error {
				old.Commit = strings.Repeat("d", 40)
				return tx.Put("candidate", old.ID, old)
			}); err != nil {
				t.Fatal(err)
			}
			if _, err = f.s.TaskAction(ctx, f.owner, "resume", task.ID, "resume", domain.VersionRequest{ExpectedVersion: task.Version}); err != nil {
				t.Fatal(err)
			}
			raw, err = f.s.Claim(ctx, f.node, "refuse-changed-source", domain.ClaimRequest{AssignmentContextVersion: domain.AssignmentContextVersion,
				Capabilities: []domain.Capability{{Tool: "codex", Available: true, Structured: true, Cancel: true}}}, f.b.String())
			a := itDecode[*domain.Assignment](t, raw, err)
			if a != nil || f.snapshot(t).Tasks[0].HoldReason != "human_decision" {
				t.Fatal("changed fixed material dispatched or silently fell back to main", a)
			}
		})
	}
}
