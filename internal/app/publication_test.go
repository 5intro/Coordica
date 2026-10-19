package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/gitops"
	"coordica/internal/store"
)

// The host protocol is injected; actual Git refs/bundles still use temporary
// repositories. GitHub HTTP semantics are tested in internal/github separately.
type testPullHost struct {
	f                                         *itFixture
	creates, merges, ensures, reads, statuses int
	created                                   bool
	failEnsure                                bool
	lostMerge                                 bool
	ci                                        string
	review                                    string
	closed                                    bool
	merged                                    bool
	reviewCheck                               string
	base                                      string
	gateChanged                               bool
	onInspect                                 func()
	onStatus                                  func(string)
	failStatus                                bool
}

func (h *testPullHost) Inspect(ctx context.Context, s domain.PullRequestSpec) (domain.PullRequestStatus, error) {
	h.reads++
	if h.onInspect != nil {
		h.onInspect()
	}
	if h.base != "" {
		s.Base = h.base
	}
	state := "open"
	if h.closed {
		state = "closed"
	}
	if h.merged {
		state = "merged"
	}
	return domain.PullRequestStatus{Number: 7, URL: "https://github.com/owner/repo/pull/7", Branch: s.Branch, Head: s.Head, Base: s.Base, State: state, CI: h.ci, Review: h.review, MergeReady: state == "open" && h.ci == "success" && h.review != "changes_requested" && h.reviewCheck == "success", MergeCommit: map[bool]string{true: s.Head}[h.merged], Detail: "remote CI/review evidence", Checks: []domain.RemoteCheck{}, Reviews: []domain.RemoteReview{}}, nil
}
func (h *testPullHost) Ensure(ctx context.Context, s domain.PullRequestSpec) (domain.PullRequestStatus, error) {
	h.ensures++
	if !h.created {
		h.created = true
		h.creates++
	}
	if h.failEnsure {
		h.failEnsure = false
		return domain.PullRequestStatus{}, errors.New("lost PR response")
	}
	return h.Inspect(ctx, s)
}
func (h *testPullHost) PublishReview(_ context.Context, _ domain.PullRequestSpec, state string) error {
	h.reviewCheck = state
	h.statuses++
	if h.onStatus != nil {
		h.onStatus(state)
	}
	if h.failStatus {
		h.failStatus = false
		return errors.New("status ACK lost")
	}
	return nil
}
func (h *testPullHost) Merge(ctx context.Context, s domain.PullRequestSpec) (string, error) {
	if h.gateChanged {
		return "", domain.ErrPublicationNotReady
	}
	h.merges++
	if s.Number != 7 || h.ci != "success" || h.reviewCheck != "success" || h.review == "changes_requested" {
		return "", errors.New("unmet remote gate")
	}
	_, e := h.f.g.Publish(ctx, h.f.w.repo(), h.f.remote, "refs/heads/main", gitops.OID(s.Base), gitops.OID(s.Head))
	if e != nil {
		return "", e
	}
	h.merged = true
	if h.lostMerge {
		return "", errors.New("merge ACK lost")
	}
	return s.Head, nil
}

func TestGitHubMainMoveRebuildsAndReusesPR(t *testing.T) {
	f, h := hostedFixture(t)
	ctx := context.Background()
	f.implement(t, "src/feature.txt")
	if e := f.w.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	f.reviewAndValidate(t)
	old := f.candidate(t)
	other := filepath.Join(t.TempDir(), "concurrent")
	if e := f.g.Clone(ctx, f.remote, other); e != nil {
		t.Fatal(e)
	}
	if e := f.g.Checkout(ctx, other, f.b); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(other, "src", "other.txt"), []byte("other\n"), 0600); e != nil {
		t.Fatal(e)
	}
	head, e := f.g.CommitAll(ctx, other, "other main", gitops.Identity{})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.g.Publish(ctx, other, f.remote, "refs/heads/main", f.b, head); e != nil {
		t.Fatal(e)
	}
	h.base = string(head)
	refreshHosted(t, f)
	if c := f.candidate(t); c.State != "accepted" || c.Commit != "" || c.Review != nil {
		t.Fatal("stale main evidence retained", c.State)
	}
	for i := 0; i < 3; i++ {
		if e = f.w.Tick(ctx); e != nil {
			t.Fatal(e)
		}
	}
	current := f.candidate(t)
	if current.Commit == old.Commit || current.Main != string(head) || current.PullRequest == nil || current.PullRequest.Number != 7 || h.creates != 1 {
		t.Fatal("rebuilt candidate lost PR binding", current.State, h.creates)
	}
}

func TestGitHubGateChangeDoesNotConsumeTaskRetry(t *testing.T) {
	f, h := hostedFixture(t)
	f.implement(t, "src/feature.txt")
	if e := f.w.Tick(context.Background()); e != nil {
		t.Fatal(e)
	}
	f.reviewAndValidate(t)
	before := f.snapshot(t).Tasks[0].Retries
	h.ci = "success"
	h.gateChanged = true
	refreshHosted(t, f)
	if f.snapshot(t).Tasks[0].Retries != before || f.candidate(t).State != "ready_to_land" {
		t.Fatal("waiting for host gates spent a task retry")
	}
}

func TestGitHubExternalMergeIsObservedWithoutSecondMerge(t *testing.T) {
	f, h := hostedFixture(t)
	f.implement(t, "src/feature.txt")
	if e := f.w.Tick(context.Background()); e != nil {
		t.Fatal(e)
	}
	f.reviewAndValidate(t)
	c := f.candidate(t)
	if _, e := f.g.Publish(context.Background(), f.w.repo(), f.remote, "refs/heads/main", gitops.OID(c.Main), gitops.OID(c.Commit)); e != nil {
		t.Fatal(e)
	}
	h.merged = true
	refreshHosted(t, f)
	if f.candidate(t).State != "landed" || h.merges != 0 || f.candidate(t).PullRequest.State != "merged" {
		t.Fatal("external merge was repeated or not reconciled")
	}
}
func hostedFixture(t *testing.T) (*itFixture, *testPullHost) {
	t.Helper()
	f := newITFixture(t)
	if e := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
		p, e := getProject(tx)
		if e != nil {
			return e
		}
		// A local bare Git transport substitutes for the remote host in this layer.
		p.Publication = &domain.PublicationPolicy{Mode: "github", RequiredChecks: []string{"ci"}}
		return tx.Put("project", p.ID, p)
	}); e != nil {
		t.Fatal(e)
	}
	h := &testPullHost{f: f, ci: "pending", review: "none"}
	f.w.PullRequests = h
	return f, h
}
func refreshHosted(t *testing.T, f *itFixture) {
	t.Helper()
	c := f.candidate(t)
	_, e := f.s.RequestPullRequestSync(context.Background(), f.owner, ID("refresh_"), c.ID, domain.VersionRequest{ExpectedVersion: c.Version})
	if e != nil {
		t.Fatal(e)
	}
	if e = f.w.Tick(context.Background()); e != nil {
		t.Fatal(e)
	}
}
func TestGitHubCandidateUsesOneReviewAndExactLanding(t *testing.T) {
	for _, lost := range []bool{false, true} {
		t.Run(map[bool]string{false: "normal", true: "lost_merge_ACK"}[lost], func(t *testing.T) {
			f, h := hostedFixture(t)
			h.lostMerge = lost
			f.implement(t, "src/feature.txt")
			b, e := f.s.Claim(context.Background(), f.node, ID("before_pr_"), domain.ClaimRequest{Capabilities: []domain.Capability{{Tool: "claude", Available: true, Structured: true, Cancel: true, Review: true}}}, string(f.b))
			if e != nil || string(b) != "null" {
				t.Fatal("review claimed before PR binding", string(b), e)
			}
			if e = f.w.Tick(context.Background()); e != nil {
				t.Fatal(e)
			}
			f.reviewAndValidate(t)
			if e = f.w.Tick(context.Background()); e != nil {
				t.Fatal(e)
			}
			if h.merges != 0 || f.candidate(t).State != "ready_to_land" {
				t.Fatal("merged with pending CI")
			}
			h.ci = "success"
			refreshHosted(t, f)
			c := f.candidate(t)
			if c.State != "landed" || h.merges != 1 || h.creates != 1 {
				t.Fatal("publication not reconciled", c.State, h.merges, h.creates)
			}
			actual, e := f.g.Query(context.Background(), f.remote, "refs/heads/main")
			if e != nil || string(actual) != c.Commit {
				t.Fatal("wrong landed candidate", actual, e)
			}
			if e = f.w.Tick(context.Background()); e != nil {
				t.Fatal(e)
			}
			if h.merges != 1 {
				t.Fatal("repeated acknowledged merge")
			}
		})
	}
}
func TestGitHubPRBindingSurvivesFailedResponseAndCenterRestart(t *testing.T) {
	f, h := hostedFixture(t)
	f.implement(t, "src/feature.txt")
	h.failEnsure = true
	if e := f.w.Tick(context.Background()); e != nil {
		t.Fatal(e)
	}
	if h.creates != 1 || f.candidate(t).PullRequest.MergeReady {
		t.Fatal("uncertain create was accepted")
	}
	if e := f.w.Tick(context.Background()); e != nil {
		t.Fatal(e)
	}
	if h.ensures != 1 {
		t.Fatal("failed synchronization ignored persisted backoff")
	}
	f.w = &Integrator{Service: f.s, Git: f.g, PullRequests: h}
	refreshHosted(t, f)
	if h.creates != 1 || f.candidate(t).PullRequest.Number != 7 {
		t.Fatal("restart created another PR")
	}
}

func TestGitHubProtectedCandidateReusesExistingOwnerApproval(t *testing.T) {
	f, h := hostedFixture(t)
	ctx := context.Background()
	f.implement(t, "src/feature.txt")
	if e := f.w.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	f.reviewAndValidate(t)
	protected := f.candidate(t)
	if e := f.s.DB.Update(ctx, func(tx *store.Tx) error {
		protected.Protected = true
		return tx.Put("candidate", protected.ID, protected)
	}); e != nil {
		t.Fatal(e)
	}
	h.ci = "success"
	refreshHosted(t, f)
	if h.merges != 0 || h.reviewCheck != "pending" {
		t.Fatal("protected candidate bypassed original Owner approval")
	}
	c := f.candidate(t)
	if _, e := f.s.ApproveCandidate(ctx, f.owner, "approve-fixed-candidate", c.ID, domain.VersionRequest{ExpectedVersion: c.Version}); e != nil {
		t.Fatal(e)
	}
	refreshHosted(t, f)
	if h.merges != 1 || h.reviewCheck != "success" || f.candidate(t).State != "landed" {
		t.Fatal("existing approval did not satisfy hosted publication")
	}
}

func TestGitHubPendingBranchReceiptCanBeReconciled(t *testing.T) {
	f, _ := hostedFixture(t)
	f.implement(t, "src/feature.txt")
	if e := f.w.Tick(context.Background()); e != nil {
		t.Fatal(e)
	}
	c := f.candidate(t)
	var prior pullRequestBinding
	if e := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
		if e := tx.Get("pull_request", c.TaskID, &prior); e != nil {
			return e
		}
		prior.Head = ""
		return tx.Put("pull_request", c.TaskID, prior)
	}); e != nil {
		t.Fatal(e)
	}
	if e := f.w.reconcileWorkBranch(context.Background(), *f.snapshot(t).Project, prior); e != nil {
		t.Fatal(e)
	}
	var current pullRequestBinding
	if e := f.s.DB.View(context.Background(), func(tx *store.Tx) error { return tx.Get("pull_request", c.TaskID, &current) }); e != nil {
		t.Fatal(e)
	}
	if current.Head != c.Commit || current.Desired != c.Commit || current.Number != 7 {
		t.Fatal("branch receipt lost fixed value or PR identity")
	}
}

func TestGitHubAuthorityMigrationFromV1DrainsOldGrant(t *testing.T) {
	f := newServiceFixture(t)
	run := f.claim(t, "old-v1-run")
	if e := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
		p, e := getProject(tx)
		if e != nil {
			return e
		}
		paths := []string{}
		for _, path := range p.ProtectedPaths {
			if path != "internal/github/**" {
				paths = append(paths, path)
			}
		}
		p.ProtectedPaths = paths
		if e = tx.Put("project", p.ID, p); e != nil {
			return e
		}
		return tx.Put("authority_policy_migration", "current", struct {
			Version int `json:"version"`
		}{1})
	}); e != nil {
		t.Fatal(e)
	}
	if e := f.s.MigrateAuthorityPolicy(context.Background()); e != nil {
		t.Fatal(e)
	}
	snap := f.snapshot(t)
	if !snap.Project.Paused || snap.Runs[0].ID != run.Run.ID || snap.Runs[0].Quiescent || snap.Runs[0].State != "cancel_requested" || !containsPaths(snap.Project.ProtectedPaths, []string{"internal/github/**"}) {
		t.Fatal("v1 migration omitted new authority path or fabricated stop")
	}
}

func TestGitHubV1MigrationInvalidatesEvenWhenAllPathsAlreadyExist(t *testing.T) {
	f := newServiceFixture(t)
	run := f.claim(t, "v1-complete-paths")
	before := f.snapshot(t)
	if e := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
		return tx.Put("authority_policy_migration", "current", struct {
			Version int `json:"version"`
		}{1})
	}); e != nil {
		t.Fatal(e)
	}
	if e := f.s.MigrateAuthorityPolicy(context.Background()); e != nil {
		t.Fatal(e)
	}
	after := f.snapshot(t)
	if !after.Project.Paused || after.Project.PolicyVersion != before.Project.PolicyVersion+1 || after.Initialization.ApprovedBaseline != nil || after.Initialization.ApprovedVersion != 0 || after.Tasks[0].ApprovedVersion != 0 || after.Tasks[0].AuthorizationVersion <= before.Tasks[0].AuthorizationVersion || after.Runs[0].ID != run.Run.ID || after.Runs[0].State != "cancel_requested" || after.Runs[0].Quiescent {
		t.Fatal("path equality grandfathered a v1 baseline or fabricated stop")
	}
	if e := f.s.MigrateAuthorityPolicy(context.Background()); e != nil {
		t.Fatal(e)
	}
	again := f.snapshot(t)
	if again.Project.Version != after.Project.Version || len(again.Events) != len(after.Events) {
		t.Fatal("v2 migration replay is not idempotent")
	}
}
func TestGitHubRemoteFailureRepairsSameTaskAndPR(t *testing.T) {
	for _, cause := range []string{"ci", "review"} {
		t.Run(cause, func(t *testing.T) {
			f, h := hostedFixture(t)
			f.implement(t, "src/feature.txt")
			if e := f.w.Tick(context.Background()); e != nil {
				t.Fatal(e)
			}
			f.reviewAndValidate(t)
			if cause == "ci" {
				h.ci = "failure"
			} else {
				h.review = "changes_requested"
			}
			refreshHosted(t, f)
			before := f.snapshot(t)
			old := f.candidate(t)
			if old.State != "rejected" || before.Tasks[0].State != "ready" || before.Tasks[0].Repairs != 1 || h.merges != 0 {
				t.Fatal("repair not bounded to original task", before.Tasks[0], old.State)
			}
			var binding pullRequestBinding
			if e := f.s.DB.View(context.Background(), func(tx *store.Tx) error { return tx.Get("pull_request", old.TaskID, &binding) }); e != nil {
				t.Fatal(e)
			}
			if binding.Number != 7 || binding.TaskID != old.TaskID {
				t.Fatal("repair lost stable PR")
			}
			// Simulate the next already checked fixed candidate, retaining the same task.
			if e := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
				var task domain.Task
				if e := tx.Get("task", old.TaskID, &task); e != nil {
					return e
				}
				next := old
				next.ID = ID("cand_")
				next.State = "reviewing"
				next.Review = nil
				next.PullRequest = nil
				task.State, task.CurrentCandidateID = "awaiting_integration", next.ID
				if e := tx.Put("task", task.ID, task); e != nil {
					return e
				}
				return tx.Put("candidate", next.ID, next)
			}); e != nil {
				t.Fatal(e)
			}
			h.ci, h.review = "pending", "none"
			if e := f.w.Tick(context.Background()); e != nil {
				t.Fatal(e)
			}
			if h.creates != 1 {
				t.Fatal("repair opened a second PR")
			}
		})
	}
}
func TestGitHubPauseAllowsOnlyObservation(t *testing.T) {
	f, h := hostedFixture(t)
	f.implement(t, "src/feature.txt")
	if e := f.w.Tick(context.Background()); e != nil {
		t.Fatal(e)
	}
	if e := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
		p, e := getProject(tx)
		if e != nil {
			return e
		}
		p.Paused = true
		return tx.Put("project", p.ID, p)
	}); e != nil {
		t.Fatal(e)
	}
	creates, ensures, statuses, merges := h.creates, h.ensures, h.statuses, h.merges
	h.ci = "success"
	refreshHosted(t, f)
	if h.creates != creates || h.ensures != ensures || h.statuses != statuses || h.merges != merges || f.candidate(t).State != "reviewing" {
		t.Fatal("paused observation changed execution")
	}
	if f.candidate(t).PullRequest.CI != "success" {
		t.Fatal("paused UI cannot observe CI")
	}
}
func TestGitHubWorkBranchTamperingCannotBeOverwritten(t *testing.T) {
	f, h := hostedFixture(t)
	f.implement(t, "src/feature.txt")
	if e := f.w.Tick(context.Background()); e != nil {
		t.Fatal(e)
	}
	c := f.candidate(t)
	// The expected branch receipt must still match; an unrelated remote tip is
	// never force-overwritten just because the branch has a Coordica prefix.
	itGit(t, f.g.GitPath, f.w.repo(), "push", "--force", "--", f.remote, string(f.b)+":refs/heads/coordica/"+c.TaskID)
	refreshHosted(t, f)
	actual, e := f.g.Query(context.Background(), f.remote, "refs/heads/coordica/"+c.TaskID)
	if e != nil || actual != f.b || f.candidate(t).PullRequest.MergeReady || h.merges != 0 {
		t.Fatal("tampered branch overwritten", actual, e)
	}
}
func TestPublicationConfigurationNeedsOwnerPausedQuietProject(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	p := f.snapshot(t).Project
	in := domain.PublicationRequest{ExpectedVersion: p.Version, Policy: domain.PublicationPolicy{Mode: "git", RequiredChecks: []string{}}}
	if _, e := f.s.ConfigurePublication(ctx, f.node, "node", in); e == nil {
		t.Fatal("node configured publication")
	}
	if e := f.s.DB.Update(ctx, func(tx *store.Tx) error { p.Paused = false; return tx.Put("project", p.ID, p) }); e != nil {
		t.Fatal(e)
	}
	if _, e := f.s.ConfigurePublication(ctx, f.owner, "running", in); e == nil {
		t.Fatal("live project mode changed")
	}
	if e := f.s.DB.Update(ctx, func(tx *store.Tx) error { p.Paused = true; return tx.Put("project", p.ID, p) }); e != nil {
		t.Fatal(e)
	}
	raw, e := f.s.ConfigurePublication(ctx, f.owner, "paused", in)
	got := decodeTest[domain.Project](t, raw, e)
	if !got.Paused || got.Publication.Mode != "git" {
		t.Fatal("configuration resumed project")
	}
	if got.PolicyVersion != p.PolicyVersion {
		t.Fatal("transport configuration invalidated code/baseline authority")
	}
	if e = f.s.DB.View(ctx, func(tx *store.Tx) error { return baselineGate(tx, nil, false) }); e != nil {
		t.Fatal("transport change requires duplicate baseline approval", e)
	}
	if _, e = f.s.ConfigurePublication(ctx, f.owner, "stale", in); e == nil {
		t.Fatal("stale configuration accepted")
	}
}
