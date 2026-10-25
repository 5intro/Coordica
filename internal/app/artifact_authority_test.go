package app

import (
	"context"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"coordica/internal/domain"
	"coordica/internal/store"
)

type artifactMutatingReader struct {
	source io.Reader
	change func() error
}

func (r *artifactMutatingReader) Read(p []byte) (int, error) {
	if r.change != nil {
		change := r.change
		r.change = nil
		if err := change(); err != nil {
			return 0, err
		}
	}
	return r.source.Read(p)
}

func TestArtifactUploadRevalidatesPublicAuthorityChangesDuringBody(t *testing.T) {
	for _, change := range []string{"revoke", "project pause", "task pause", "cancel", "block", "goal", "ownership", "policy", "lease", "deadline"} {
		t.Run(change, func(t *testing.T) {
			f, _, _ := requirementFixture(t)
			a := f.claim(t, "artifact-authority")
			if a == nil {
				t.Fatal("missing authorized implementation")
			}
			var before domain.Snapshot
			var changed bool
			body := &artifactMutatingReader{source: strings.NewReader("fixed result"), change: func() error {
				if err := mutateArtifactAuthority(t, f, a, change); err != nil {
					return err
				}
				changed = true
				before = f.snapshot(t)
				return nil
			}}
			_, err := f.s.UploadArtifact(context.Background(), f.node, a.Run.ID, body)
			if !changed {
				t.Fatal("public authority mutation did not run", err)
			}
			if err == nil {
				t.Fatal("accepted artifact after authority changed", change)
			}
			if !reflect.DeepEqual(before, f.snapshot(t)) {
				t.Fatal("rejected upload changed durable state, accounting or audit")
			}
		})
	}
}

func mutateArtifactAuthority(t *testing.T, f *serviceFixture, a *domain.Assignment, change string) error {
	t.Helper()
	ctx := context.Background()
	snap := f.snapshot(t)
	task := requirementTask(t, f, a.Task.ID)
	var err error
	switch change {
	case "revoke":
		_, err = f.s.RevokeNode(ctx, f.owner, "revoke-during-upload", f.node.NodeID)
	case "project pause":
		_, err = f.s.ProjectAction(ctx, f.owner, "pause-upload", "pause", domain.VersionRequest{ExpectedVersion: snap.Project.Version})
	case "task pause", "cancel":
		action := "pause"
		if change == "cancel" {
			action = "cancel"
		}
		_, err = f.s.TaskAction(ctx, f.owner, "task-upload", task.ID, action, domain.VersionRequest{ExpectedVersion: task.Version})
	case "block":
		_, err = f.s.Block(ctx, f.node, "block-upload", a.Run.ID, a.Run.Generation, "Owner decision required")
	case "goal":
		doc := *snap.PlanningDocument
		doc.Objective += " revised"
		_, err = f.s.SavePlanning(ctx, f.owner, "goal-upload", domain.PlanningSaveRequest{ExpectedVersion: doc.Version, Document: doc})
	case "ownership", "policy":
		err = replaceArtifactExecution(t, f, a, change)
	case "lease":
		f.now = a.Run.LeaseUntil
	case "deadline":
		f.now = a.Run.Deadline
	}
	return err
}

func replaceArtifactExecution(t *testing.T, f *serviceFixture, a *domain.Assignment, change string) error {
	t.Helper()
	ctx := context.Background()
	_, err := f.s.Complete(ctx, f.node, "stop-upload", a.Run.ID, domain.CompleteRequest{
		Generation: a.Run.Generation, Status: "stopped", Quiescent: true,
	})
	if err != nil {
		return err
	}
	snap := f.snapshot(t)
	if change == "policy" {
		_, err = f.s.ProjectAction(ctx, f.owner, "pause-policy-upload", "pause", domain.VersionRequest{ExpectedVersion: snap.Project.Version})
		if err != nil {
			return err
		}
		project := f.snapshot(t).Project
		_, err = f.s.UpdateProjectPolicy(ctx, f.owner, "policy-upload", domain.ProjectPolicyRequest{
			ExpectedVersion: project.Version, Checks: project.Checks, ProtectedPaths: project.ProtectedPaths,
		})
		return err
	}
	task := requirementTask(t, f, a.Task.ID)
	_, err = f.s.TaskAction(ctx, f.owner, "resume-upload", task.ID, "resume", domain.VersionRequest{ExpectedVersion: task.Version})
	if err != nil {
		return err
	}
	next := f.claim(t, "replace-upload")
	if next == nil || next.Run.ID == a.Run.ID || next.Run.Generation <= a.Run.Generation {
		t.Fatal("replacement did not advance execution identity")
	}
	return nil
}

func TestArtifactUploadRejectsRevokedActorBeforeReadingBody(t *testing.T) {
	f := newServiceFixture(t)
	a := f.claim(t, "upload")
	if _, err := f.s.RevokeNode(context.Background(), f.owner, "revoke", f.node.NodeID); err != nil {
		t.Fatal(err)
	}
	read := false
	body := &artifactMutatingReader{source: strings.NewReader("bytes"), change: func() error { read = true; return nil }}
	if _, err := f.s.UploadArtifact(context.Background(), f.node, a.Run.ID, body); err == nil || read {
		t.Fatal("revoked preflight consumed body", err, read)
	}
}

func TestArtifactUploadCannotUseCachedBindingsAsCurrentAuthority(t *testing.T) {
	for _, change := range []string{"goal", "ownership", "pause", "hold", "policy", "deadline", "grant", "budget"} {
		t.Run(change, func(t *testing.T) {
			f, _, _ := requirementFixture(t)
			a := f.claim(t, "cached")
			// Defense in depth: immutable Run copies must not substitute for current
			// authority. The public transition/race paths are covered separately.
			if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
				return changeArtifactAuthorityRecord(tx, a, change, f.now)
			}); err != nil {
				t.Fatal(err)
			}
			read := false
			body := &artifactMutatingReader{source: strings.NewReader("bytes"), change: func() error { read = true; return nil }}
			if _, err := f.s.UploadArtifact(context.Background(), f.node, a.Run.ID, body); err == nil || read {
				t.Fatal("stale cached bindings accepted or body consumed", change, err, read)
			}
		})
	}
}

func changeArtifactAuthorityRecord(tx *store.Tx, a *domain.Assignment, change string, now time.Time) error {
	switch change {
	case "goal":
		var doc domain.PlanningDocument
		if err := tx.Get("planning_document", "current", &doc); err != nil {
			return err
		}
		doc.ApprovedAuthorizationVersion = 0
		return tx.Put("planning_document", "current", doc)
	case "ownership", "hold", "budget":
		task := a.Task
		if change == "ownership" {
			task.CurrentRunID = "new-execution"
		} else if change == "hold" {
			task.HoldReason = "human_decision"
		} else {
			task.UsedSeconds = task.BudgetSeconds
		}
		return tx.Put("task", task.ID, task)
	case "pause", "policy":
		project, err := getProject(tx)
		if err != nil {
			return err
		}
		if change == "pause" {
			project.Paused = true
		} else {
			project.PolicyVersion++
		}
		return tx.Put("project", project.ID, project)
	default:
		run := a.Run
		if change == "deadline" {
			run.Deadline = now
		} else {
			run.ElapsedSeconds = run.GrantSeconds
		}
		return tx.Put("run", run.ID, run)
	}
}
