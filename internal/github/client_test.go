package github

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"coordica/internal/domain"
	"coordica/internal/localfile"
)

type protocolFixture struct {
	client                *Client
	spec                  domain.PullRequestSpec
	create, merge, status int
	created               bool
	closed                bool
	wrongHead             bool
	strict                bool
	admins                bool
	checkState            string
	reviewState           string
	checks                []map[string]any
	reviews               []map[string]any
	postLost              bool
	headRepository        string
	baseRepository        string
	mergeableState        string
	unmergeable           bool
	reviewAppID           *int64
}

func protocol(t *testing.T) *protocolFixture {
	t.Helper()
	f := &protocolFixture{strict: true, admins: true, checkState: "success", reviewState: "APPROVED", spec: domain.PullRequestSpec{Repository: "owner/repo", Branch: "coordica/task_" + strings.Repeat("a", 32), TargetBranch: "main", Head: strings.Repeat("b", 40), Base: strings.Repeat("c", 40), Title: "Scoped work", RequiredChecks: []string{"ci"}}}
	path := filepath.Join(t.TempDir(), "token")
	if e := localfile.WritePrivate(path, []byte("test-token")); e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("missing fixed authorization")
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		write := func(v any) {
			if e := json.NewEncoder(w).Encode(v); e != nil {
				t.Error(e)
			}
		}
		p := strings.TrimPrefix(r.URL.Path, "/repos/owner/repo")
		pull := func() map[string]any {
			head := f.spec.Head
			if f.wrongHead {
				head = strings.Repeat("e", 40)
			}
			state := "open"
			if f.closed {
				state = "closed"
			}
			headRepository, baseRepository, mergeableState := f.headRepository, f.baseRepository, f.mergeableState
			if headRepository == "" {
				headRepository = "owner/repo"
			}
			if baseRepository == "" {
				baseRepository = "owner/repo"
			}
			if mergeableState == "" {
				mergeableState = "clean"
			}
			return map[string]any{"number": 7, "state": state, "merged": false, "draft": false, "mergeable": !f.unmergeable, "mergeable_state": mergeableState, "head": map[string]any{"sha": head, "ref": f.spec.Branch, "repo": map[string]string{"full_name": headRepository}}, "base": map[string]any{"sha": f.spec.Base, "ref": "main", "repo": map[string]string{"full_name": baseRepository}}}
		}
		switch {
		case p == "/pulls" && r.Method == "GET":
			if f.created {
				write([]any{pull()})
			} else {
				write([]any{})
			}
		case p == "/pulls" && r.Method == "POST":
			f.create++
			f.created = true
			if f.postLost {
				w.WriteHeader(502)
			} else {
				write(pull())
			}
		case p == "/pulls/7" && r.Method == "GET":
			write(pull())
		case strings.HasSuffix(p, "/check-runs"):
			checks := f.checks
			if checks == nil {
				checks = []map[string]any{{"id": 1, "name": "ci", "status": "completed", "conclusion": f.checkState, "head_sha": f.spec.Head, "app": map[string]int{"id": 42}}}
			}
			write(map[string]any{"total_count": len(checks), "check_runs": checks})
		case strings.HasSuffix(p, "/statuses"):
			write([]any{map[string]any{"id": 9, "context": domain.ReviewCheckContext, "state": "success"}})
		case p == "/pulls/7/reviews":
			if f.reviews != nil {
				write(f.reviews)
			} else {
				write([]any{map[string]any{"id": 3, "state": f.reviewState, "commit_id": f.spec.Head, "user": map[string]string{"login": "reviewer"}}})
			}
		case p == "/branches/main/protection":
			write(map[string]any{"enforce_admins": map[string]bool{"enabled": f.admins}, "required_status_checks": map[string]any{"strict": f.strict, "checks": []any{map[string]any{"context": "ci", "app_id": 42}, map[string]any{"context": domain.ReviewCheckContext, "app_id": f.reviewAppID}}}})
		case p == "/pulls/7/merge" && r.Method == "PUT":
			var in map[string]string
			if e := json.NewDecoder(r.Body).Decode(&in); e != nil {
				t.Error(e)
			}
			if in["sha"] != f.spec.Head || in["merge_method"] != "merge" {
				t.Error("unfenced or non-preserving merge")
			}
			f.merge++
			write(map[string]any{"merged": true, "sha": strings.Repeat("d", 40)})
		case strings.HasPrefix(p, "/statuses/") && r.Method == "POST":
			f.status++
			write(map[string]any{})
		default:
			t.Errorf("unexpected GitHub request %s %s", r.Method, p)
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(server.Close)
	f.client = &Client{repository: "owner/repo", tokenFile: path, baseURL: server.URL, http: server.Client()}
	return f
}
func TestPRCreationLostResponseAndRepairReuseOriginal(t *testing.T) {
	f := protocol(t)
	f.postLost = true
	for i := 0; i < 2; i++ {
		facts, e := f.client.Ensure(context.Background(), f.spec)
		if e != nil || facts.Number != 7 {
			t.Fatal(facts, e)
		}
	}
	if f.create != 1 {
		t.Fatal("duplicate PR created", f.create)
	}
	f.spec.Head = strings.Repeat("e", 40)
	facts, e := f.client.Ensure(context.Background(), f.spec)
	if e != nil || facts.Number != 7 || f.create != 1 || facts.Head != f.spec.Head {
		t.Fatal("repair did not reuse original PR", facts, e)
	}
}
func TestPRMergeUsesCurrentHeadAndRequiredGates(t *testing.T) {
	for _, kind := range []string{"pass", "ci-failed", "ci-cancelled", "ci-skipped", "ci-action-required", "strict-disabled", "admin-bypass", "wrong-head", "changes-requested", "closed"} {
		t.Run(kind, func(t *testing.T) {
			f := protocol(t)
			f.spec.Number = 7
			switch kind {
			case "ci-failed":
				f.checkState = "failure"
			case "ci-cancelled":
				f.checkState = "cancelled"
			case "ci-skipped":
				f.checkState = "skipped"
			case "ci-action-required":
				f.checkState = "action_required"
			case "strict-disabled":
				f.strict = false
			case "admin-bypass":
				f.admins = false
			case "wrong-head":
				f.wrongHead = true
			case "changes-requested":
				f.reviewState = "CHANGES_REQUESTED"
			case "closed":
				f.closed = true
			}
			_, e := f.client.Merge(context.Background(), f.spec)
			if kind == "pass" {
				if e != nil || f.merge != 1 {
					t.Fatal(e, f.merge)
				}
			} else if e == nil || f.merge != 0 {
				t.Fatal("unsafe merge", kind, e)
			}
		})
	}
}
func TestPRCheckSourceCannotBeSpoofedAndMissingIsNotSuccess(t *testing.T) {
	f := protocol(t)
	f.spec.Number = 7
	f.checks = []map[string]any{{"id": 1, "name": "ci", "status": "completed", "conclusion": "failure", "head_sha": f.spec.Head, "app": map[string]int{"id": 42}}, {"id": 2, "name": "ci", "status": "completed", "conclusion": "success", "head_sha": f.spec.Head, "app": map[string]int{"id": 99}}}
	facts, e := f.client.Inspect(context.Background(), f.spec)
	if e != nil || facts.CI != "failure" || facts.MergeReady {
		t.Fatal("spoofed check passed", facts, e)
	}
	f.checks = []map[string]any{}
	facts, e = f.client.Inspect(context.Background(), f.spec)
	if e != nil || facts.CI == "success" || facts.MergeReady {
		t.Fatal("missing check passed", facts, e)
	}
}
func TestPRFixedRepositoryAndCredentialErrorsAreRedacted(t *testing.T) {
	f := protocol(t)
	f.spec.Repository = "other/repo"
	if _, e := f.client.Ensure(context.Background(), f.spec); e == nil {
		t.Fatal("repository binding lost")
	}
	f.spec.Repository = "owner/repo"
	f.client.tokenFile = filepath.Join(t.TempDir(), "missing")
	_, e := f.client.Ensure(context.Background(), f.spec)
	if e == nil || strings.Contains(fmt.Sprint(e), f.client.tokenFile) || strings.Contains(fmt.Sprint(e), "test-token") {
		t.Fatal("credential error leaks secret/path", e)
	}
}

func TestPRLatestFailedAttemptCannotUseOlderSuccess(t *testing.T) {
	f := protocol(t)
	f.spec.Number = 7
	f.checks = []map[string]any{{"id": 1, "name": "ci", "status": "completed", "conclusion": "success", "head_sha": f.spec.Head, "app": map[string]int{"id": 42}}, {"id": 2, "name": "ci", "status": "completed", "conclusion": "failure", "head_sha": f.spec.Head, "app": map[string]int{"id": 42}}}
	facts, e := f.client.Inspect(context.Background(), f.spec)
	if e != nil || facts.CI != "failure" || facts.MergeReady {
		t.Fatal("old success overrode later failure", facts, e)
	}
}

func TestPRRejectsInvalidSHABeforeNetwork(t *testing.T) {
	f := protocol(t)
	f.spec.Head = strings.Repeat("/", 40)
	if _, e := f.client.Ensure(context.Background(), f.spec); e == nil || f.create != 0 {
		t.Fatal("invalid object ID reached GitHub")
	}
}

func TestPRClosedIsNotReopenedOrRecreated(t *testing.T) {
	f := protocol(t)
	f.created = true
	f.closed = true
	facts, e := f.client.Ensure(context.Background(), f.spec)
	if e != nil || facts.State != "closed" || f.create != 0 {
		t.Fatal("closed PR recreated", facts, e)
	}
}

func TestPRReviewPublishesOnlyStatusNotAnotherApproval(t *testing.T) {
	f := protocol(t)
	for _, state := range []string{"pending", "success", "failure"} {
		if e := f.client.PublishReview(context.Background(), f.spec, state); e != nil {
			t.Fatal(e)
		}
	}
	if f.status != 3 || f.create != 0 || f.merge != 0 {
		t.Fatal("review status caused extra PR or merge")
	}
}

func TestPRNeverFollowsRedirectWithCredential(t *testing.T) {
	f := protocol(t)
	followed := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { followed = true; w.WriteHeader(200) }))
	t.Cleanup(target.Close)
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(redirect.Close)
	c, e := New("owner/repo", f.client.tokenFile)
	if e != nil {
		t.Fatal(e)
	}
	c.baseURL = redirect.URL
	if _, e = c.Ensure(context.Background(), f.spec); e == nil || followed {
		t.Fatal("credential-bearing request followed redirect")
	}
}

func TestPRWrongCommitCheckCannotPass(t *testing.T) {
	f := protocol(t)
	f.spec.Number = 7
	f.checks = []map[string]any{{"id": 1, "name": "ci", "status": "completed", "conclusion": "success", "head_sha": strings.Repeat("e", 40), "app": map[string]int{"id": 42}}}
	if facts, e := f.client.Inspect(context.Background(), f.spec); e == nil || facts.MergeReady {
		t.Fatal("foreign commit check was accepted")
	}
}

func TestGitHubFeedbackIsBoundedValidUTF8(t *testing.T) {
	remaining := 5000
	first := boundedFeedback(strings.Repeat("你好", 2000), &remaining)
	second := boundedFeedback(strings.Repeat("x", 5000), &remaining)
	if !utf8.ValidString(first) || len(first) > 4096 || len(first)+len(second) > 5000 || remaining != 0 {
		t.Fatal("feedback exceeded shared byte budget")
	}
}

func TestDraftAndCommentCannotDismissChangesRequested(t *testing.T) {
	f := protocol(t)
	f.spec.Number = 7
	f.reviews = []map[string]any{}
	for i, state := range []string{"CHANGES_REQUESTED", "COMMENTED", "PENDING"} {
		f.reviews = append(f.reviews, map[string]any{"id": i + 1, "state": state, "commit_id": f.spec.Head, "user": map[string]string{"login": "reviewer"}})
	}
	facts, e := f.client.Inspect(context.Background(), f.spec)
	if e != nil || facts.Review != "changes_requested" || facts.MergeReady {
		t.Fatal("unsubmitted draft or comment cleared required changes", facts, e)
	}
}

func TestGitAuthorizationStaysBoundToConfiguredRepository(t *testing.T) {
	f := protocol(t)
	value, e := f.client.HTTPSAuthorization("https://github.com/owner/repo.git")
	if e != nil {
		t.Fatal(e)
	}
	raw, e := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, "Basic "))
	if e != nil || string(raw) != "x-access-token:test-token" {
		t.Fatal("invalid Git credential format")
	}
	for _, remote := range []string{"https://github.com/other/repo.git", "https://example.com/owner/repo.git", "https://github.com/owner/repo.git?redirect=1"} {
		if _, e = f.client.HTTPSAuthorization(remote); e == nil {
			t.Fatal("credential allowed outside fixed repository")
		}
	}
}

func TestPRRepositoryCasingPreservesIdentityBoundary(t *testing.T) {
	for _, kind := range []string{"same_repository", "other_head_owner", "other_base_repository"} {
		t.Run(kind, func(t *testing.T) {
			f := protocol(t)
			f.spec.Number, f.spec.Repository = 7, "OWNER/REPO"
			f.headRepository, f.baseRepository = "Owner/RePo", "OWNER/repo"
			if kind == "other_head_owner" {
				f.headRepository = "other/repo"
			}
			if kind == "other_base_repository" {
				f.baseRepository = "owner/repo-extra"
			}
			_, e := f.client.Inspect(context.Background(), f.spec)
			if (e == nil) != (kind == "same_repository") {
				t.Fatal("repository identity comparison is incorrect", e)
			}
		})
	}
	f := protocol(t)
	if _, e := f.client.HTTPSAuthorization("https://github.com/OWNER/RePo.git"); e != nil {
		t.Fatal("equivalent repository casing rejected for Git authorization", e)
	}
}

func TestPROptionalFailedChecksPreserveRequiredGates(t *testing.T) {
	for _, kind := range []string{"optional_only", "required_failed", "unmergeable"} {
		t.Run(kind, func(t *testing.T) {
			f := protocol(t)
			f.spec.Number, f.mergeableState = 7, "unstable"
			conclusion := "success"
			if kind == "required_failed" {
				conclusion = "failure"
			}
			f.unmergeable = kind == "unmergeable"
			f.checks = []map[string]any{
				{"id": 1, "name": "ci", "status": "completed", "conclusion": conclusion, "head_sha": f.spec.Head, "app": map[string]int{"id": 42}},
				{"id": 2, "name": "optional", "status": "completed", "conclusion": "failure", "head_sha": f.spec.Head, "app": map[string]int{"id": 42}},
			}
			_, e := f.client.Merge(context.Background(), f.spec)
			if kind == "optional_only" {
				if e != nil || f.merge != 1 {
					t.Fatal("optional failure blocked a mergeable PR", e)
				}
			} else if e == nil || f.merge != 0 {
				t.Fatal("optional-check handling bypassed required gates", e)
			}
		})
	}
}

func TestPRTokenRejectsDirectory(t *testing.T) {
	f := protocol(t)
	f.client.tokenFile = t.TempDir()
	if _, e := f.client.token(); e == nil || strings.Contains(e.Error(), f.client.tokenFile) {
		t.Fatal("non-regular token accepted or path disclosed", e)
	}
}

func TestPRAppBoundLegacyStatusHasExplicitUnsupportedReason(t *testing.T) {
	f := protocol(t)
	f.spec.Number = 7
	appID := int64(42)
	f.reviewAppID = &appID
	facts, e := f.client.Inspect(context.Background(), f.spec)
	if e != nil || facts.MergeReady || facts.CI == "success" || !strings.Contains(facts.Detail, "legacy commit status") || !strings.Contains(facts.Detail, domain.ReviewCheckContext) {
		t.Fatal("unknown legacy source was not explicitly rejected", facts, e)
	}
	// A pinned Check Run has verifiable App identity, even alongside a same-name
	// legacy status. Unknown legacy sources cannot stand in for that App.
	f.checks = []map[string]any{
		{"id": 1, "name": "ci", "status": "completed", "conclusion": "success", "head_sha": f.spec.Head, "app": map[string]int{"id": 42}},
		{"id": 2, "name": domain.ReviewCheckContext, "status": "completed", "conclusion": "success", "head_sha": f.spec.Head, "app": map[string]int{"id": 42}},
	}
	facts, e = f.client.Inspect(context.Background(), f.spec)
	if e != nil || !facts.MergeReady {
		t.Fatal("verifiable pinned Check Run was rejected", facts, e)
	}
}
