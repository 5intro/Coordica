package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func nodeConsoleTestActor(t *testing.T, f *serviceFixture, name string) Actor {
	t.Helper()
	ctx := context.Background()
	raw, err := f.s.CreatePairing(ctx, f.owner, "pair-"+name, name)
	pairing := decodeTest[map[string]string](t, raw, err)
	registration, err := f.s.Register(ctx, domain.RegisterRequest{Code: pairing["code"], Name: name, Platform: "test", Capacity: 1})
	if err != nil {
		t.Fatal(err)
	}
	actor, err := f.s.Authenticate(ctx, registration.Token)
	if err != nil {
		t.Fatal(err)
	}
	return actor
}

func assertNodeConsoleError(t *testing.T, err error, code string) {
	t.Helper()
	var failure *domain.Error
	if !errors.As(err, &failure) || failure.Code != code {
		t.Fatalf("error = %v, want %s", err, code)
	}
}

func TestNodeSnapshotOnlyReturnsOwnNodeWorkAndProposals(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	other := nodeConsoleTestActor(t, f, "Other node")
	assignment := f.claim(t, "own-run")
	if assignment == nil {
		t.Fatal("own task was not claimed")
	}
	raw, err := f.s.CreateRequirement(ctx, f.owner, "owner-requirement", domain.Requirement{Title: "owner-private-title", Body: "owner-private-body"})
	ownerRequirement := decodeTest[domain.Requirement](t, raw, err)
	raw, err = f.s.SubmitRequirement(ctx, f.node, "own-requirement", domain.RequirementProposal{Title: "Own proposal", Body: "Consider this feature"})
	ownRequirement := decodeTest[domain.Requirement](t, raw, err)
	raw, err = f.s.SubmitRequirement(ctx, other, "other-requirement", domain.RequirementProposal{Title: "other-private-title", Body: "other-private-body"})
	otherRequirement := decodeTest[domain.Requirement](t, raw, err)
	if err = f.s.DB.Update(ctx, func(tx *store.Tx) error {
		project, err := getProject(tx)
		if err != nil {
			return err
		}
		project.Paused = true
		project.RepositoryURL = "/private/owner-repository"
		if err := tx.Put("project", project.ID, project); err != nil {
			return err
		}
		for _, record := range []struct {
			kind, id string
			value    any
		}{
			{"task", "other_task", domain.Task{ID: "other_task", Title: "other-private-task"}},
			{"task", "unclaimed_task", domain.Task{ID: "unclaimed_task", Title: "unclaimed-private-task"}},
			{"run", "other_run", domain.Run{ID: "other_run", TaskID: "other_task", NodeID: other.NodeID, Detail: "other-private-run"}},
			{"candidate", "private_candidate", domain.Candidate{ID: "private_candidate", Detail: "owner-private-candidate"}},
		} {
			if err := tx.Put(record.kind, record.id, record.value); err != nil {
				return err
			}
		}
		return f.s.audit(tx, f.owner, "owner.private", "private-event", "owner-private-event")
	}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := f.s.NodeSnapshot(ctx, f.node)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.APIVersion != domain.APIVersion || snapshot.Node.ID != f.node.NodeID || snapshot.ProjectName != "Test" || !snapshot.ProjectPaused {
		t.Fatalf("wrong node/project metadata: %+v", snapshot)
	}
	if len(snapshot.Tasks) != 1 || snapshot.Tasks[0].ID != assignment.Task.ID || len(snapshot.Runs) != 1 || snapshot.Runs[0].ID != assignment.Run.ID {
		t.Fatalf("snapshot crossed work ownership: %+v", snapshot)
	}
	if len(snapshot.Requirements) != 1 || snapshot.Requirements[0].ID != ownRequirement.ID {
		t.Fatalf("snapshot crossed proposal ownership: %+v", snapshot.Requirements)
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{other.NodeID, otherRequirement.ID, ownerRequirement.ID, "other-private", "unclaimed-private", "owner-private", "/private/owner-repository", `"events"`, `"candidates"`, `"nodes"`, `"repository_url"`, `"checks"`, `"token"`, `"token_hash"`} {
		if strings.Contains(string(encoded), private) {
			t.Fatalf("node snapshot exposed %q: %s", private, encoded)
		}
	}
}

func TestNodeSnapshotAndRequirementSubmissionRejectInvalidActors(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	other := nodeConsoleTestActor(t, f, "Other")
	for name, actor := range map[string]Actor{
		"owner":        f.owner,
		"anonymous":    {},
		"missing node": {ID: "missing", Role: "node", NodeID: "missing"},
		"forged node":  {ID: f.node.ID, Role: "node", NodeID: other.NodeID},
	} {
		t.Run(name, func(t *testing.T) {
			snapshot, err := f.s.NodeSnapshot(ctx, actor)
			assertNodeConsoleError(t, err, "forbidden")
			if snapshot.Node.ID != "" || len(snapshot.Runs) != 0 || len(snapshot.Tasks) != 0 || len(snapshot.Requirements) != 0 {
				t.Fatal("failed authorization returned node data")
			}
			_, err = f.s.SubmitRequirement(ctx, actor, "unauthorized-"+name, domain.RequirementProposal{Title: "Unauthorized", Body: "Must not be recorded"})
			assertNodeConsoleError(t, err, "forbidden")
		})
	}
	if len(f.snapshot(t).Requirements) != 0 {
		t.Fatal("unauthorized submission created a requirement")
	}
	if _, err := f.s.RevokeNode(ctx, f.owner, "revoke", f.node.NodeID); err != nil {
		t.Fatal(err)
	}
	_, err := f.s.NodeSnapshot(ctx, f.node)
	assertNodeConsoleError(t, err, "forbidden")
	_, err = f.s.SubmitRequirement(ctx, f.node, "revoked-submit", domain.RequirementProposal{Title: "Revoked", Body: "Must not be recorded"})
	assertNodeConsoleError(t, err, "forbidden")
}

func TestRequirementProposalReplayPreservesOriginAuditAndTaskAuthority(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	before := f.snapshot(t)
	other := nodeConsoleTestActor(t, f, "Other")
	var proposal domain.RequirementProposal
	// Source identity is not part of this input contract; only the authenticated
	// node may set the persisted origin, even if a caller decodes extra fields.
	if err := json.Unmarshal([]byte(`{"title":"  新功能  ","body":"  请考虑这个需求  ","source_node_id":"`+other.NodeID+`"}`), &proposal); err != nil {
		t.Fatal(err)
	}
	raw, err := f.s.SubmitRequirement(ctx, f.node, "proposal", proposal)
	requirement := decodeTest[domain.Requirement](t, raw, err)
	if requirement.SourceNodeID != f.node.NodeID || requirement.Title != "新功能" || requirement.Body != "请考虑这个需求" || requirement.Version != 1 {
		t.Fatalf("incorrect proposal record: %+v", requirement)
	}
	replayed, err := f.s.SubmitRequirement(ctx, f.node, "proposal", proposal)
	if err != nil || string(replayed) != string(raw) {
		t.Fatalf("proposal replay changed: %s %v", replayed, err)
	}
	proposal.Body = "Changed proposal"
	_, err = f.s.SubmitRequirement(ctx, f.node, "proposal", proposal)
	assertNodeConsoleError(t, err, "idempotency_conflict")
	after := f.snapshot(t)
	if len(after.Requirements) != 1 || len(after.Runs) != 0 || len(after.Candidates) != 0 {
		t.Fatalf("proposal duplicated or dispatched work: %+v", after)
	}
	beforeTasks, _ := json.Marshal(before.Tasks)
	afterTasks, _ := json.Marshal(after.Tasks)
	if string(beforeTasks) != string(afterTasks) || after.Project.UsedSeconds != before.Project.UsedSeconds || after.Project.ReservedSeconds != before.Project.ReservedSeconds {
		t.Fatal("proposal altered task authorization or execution budget")
	}
	auditCount := 0
	for _, event := range after.Events {
		if event.Kind == "requirement.submitted" {
			auditCount++
			if event.Actor != f.node.ID || event.ObjectID != requirement.ID {
				t.Fatalf("incorrect proposal audit: %+v", event)
			}
		}
	}
	if auditCount != 1 {
		t.Fatalf("proposal emitted %d audit records", auditCount)
	}
	if _, err = f.s.RevokeNode(ctx, f.owner, "revoke-after-submit", f.node.NodeID); err != nil {
		t.Fatal(err)
	}
	proposal.Body = "  请考虑这个需求  "
	_, err = f.s.SubmitRequirement(ctx, f.node, "proposal", proposal)
	assertNodeConsoleError(t, err, "forbidden")
}

func TestRequirementProposalValidationAndOwnerOriginCannotBeForged(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	for name, proposal := range map[string]domain.RequirementProposal{
		"empty title":   {Title: " \t ", Body: "body"},
		"empty body":    {Title: "title", Body: " \n "},
		"long title":    {Title: strings.Repeat("中", 201), Body: "body"},
		"long body":     {Title: "title", Body: strings.Repeat("x", (64<<10)+1)},
		"invalid UTF-8": {Title: "title", Body: string([]byte{0xff})},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := f.s.SubmitRequirement(ctx, f.node, "invalid-"+name, proposal)
			assertNodeConsoleError(t, err, "invalid_input")
		})
	}
	if len(f.snapshot(t).Requirements) != 0 {
		t.Fatal("invalid proposals were persisted")
	}
	raw, err := f.s.SubmitRequirement(ctx, f.node, "limit", domain.RequirementProposal{Title: strings.Repeat("中", 200), Body: strings.Repeat("x", 64<<10)})
	decodeTest[domain.Requirement](t, raw, err)
	raw, err = f.s.CreateRequirement(ctx, f.owner, "owner-forged-origin", domain.Requirement{Title: "Owner requirement", Body: "Owner body", SourceNodeID: f.node.NodeID})
	requirement := decodeTest[domain.Requirement](t, raw, err)
	if requirement.SourceNodeID != "" {
		t.Fatal("owner input forged a node origin")
	}
	snapshot, err := f.s.NodeSnapshot(ctx, f.node)
	if err != nil || len(snapshot.Requirements) != 1 || snapshot.Requirements[0].ID == requirement.ID {
		t.Fatalf("owner-origin forgery entered node snapshot: %+v %v", snapshot, err)
	}
}

func TestProjectAlwaysProtectsLocalConsoleBoundaries(t *testing.T) {
	f := newServiceFixture(t)
	project := f.snapshot(t).Project
	for _, required := range []string{"internal/localhttp/**", "internal/nodeconsole/**"} {
		found := false
		for _, protected := range project.ProtectedPaths {
			if protected == required {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("project creation left %s outside mandatory protection", required)
		}
	}
}
