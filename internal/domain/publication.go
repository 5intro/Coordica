package domain

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const ReviewCheckContext = "coordica/review"

// No merge was submitted/applied; waiting for host gates spends no Run retry.
var ErrPublicationNotReady = errors.New("publication gates not ready")

// PublicationPolicy contains no credentials. Empty mode preserves local Git.
type PublicationPolicy struct {
	ReviewProviders []ExternalReviewPolicy `json:"review_providers,omitempty"`
	Mode            string                 `json:"mode"`
	RequiredChecks  []string               `json:"required_checks"`
}

type PublicationRequest struct {
	ExpectedVersion int64             `json:"expected_version"`
	Policy          PublicationPolicy `json:"policy"`
}

// PullRequestSpec is a fixed protocol request, not a task or scheduling rule.
type PullRequestSpec struct {
	ReviewProviders []ExternalReviewPolicy
	Repository      string
	Branch          string
	TargetBranch    string
	Head            string
	Base            string
	Title           string
	Number          int
	RequiredChecks  []string
}

type RemoteCheck struct {
	ID      int64  `json:"id,omitempty"`
	Summary string `json:"summary,omitempty"`
	Name    string `json:"name"`
	State   string `json:"state"`
	AppID   int64  `json:"app_id"`
	URL     string `json:"url"`
}

type RemoteReview struct {
	Summary string `json:"summary,omitempty"`
	ID      int64  `json:"id"`
	Author  string `json:"author"`
	State   string `json:"state"`
	Commit  string `json:"commit"`
	URL     string `json:"url"`
}

// PullRequestStatus is an observed fact, never an independent execution grant.
type PullRequestStatus struct {
	ExternalReviews []ExternalReview `json:"external_reviews,omitempty"`
	Number          int              `json:"number"`
	URL             string           `json:"url"`
	Branch          string           `json:"branch"`
	Head            string           `json:"head"`
	Base            string           `json:"base"`
	State           string           `json:"state"`
	CI              string           `json:"ci"`
	Review          string           `json:"review"`
	Checks          []RemoteCheck    `json:"checks"`
	Reviews         []RemoteReview   `json:"reviews"`
	MergeCommit     string           `json:"merge_commit"`
	MergeReady      bool             `json:"merge_ready"`
	Detail          string           `json:"detail"`
	SyncError       string           `json:"sync_error,omitempty"`
	SyncedAt        time.Time        `json:"synced_at"`
}

func GitHubPublication(p Project) bool { return p.Publication != nil && p.Publication.Mode == "github" }

var githubName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,99}$`)

func GitHubRepository(remote string) (string, error) {
	u, e := url.Parse(remote)
	if e != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" {
		return "", Fail("invalid_input", "GitHub 模式需要 https://github.com/owner/repository 地址")
	}
	parts := strings.Split(strings.TrimSuffix(strings.TrimPrefix(u.Path, "/"), ".git"), "/")
	if len(parts) != 2 || !githubName.MatchString(parts[0]) || !githubName.MatchString(parts[1]) || parts[0] == "." || parts[1] == "." || strings.Contains(parts[0]+parts[1], "..") {
		return "", Fail("invalid_input", "GitHub 仓库地址无效")
	}
	return strings.Join(parts, "/"), nil
}

func ValidatePublication(p Project) error {
	if p.Publication == nil {
		return nil
	}
	policy := p.Publication
	if policy.Mode == "" || policy.Mode == "git" {
		if len(policy.RequiredChecks) != 0 || len(policy.ReviewProviders) != 0 {
			return Fail("invalid_input", "本地 Git 模式不接受 GitHub 检查配置")
		}
		return nil
	}
	if policy.Mode != "github" {
		return Fail("invalid_input", "不支持的发布模式")
	}
	if err := ValidateExternalReviewPolicy(policy.ReviewProviders); err != nil {
		return err
	}
	if _, e := GitHubRepository(p.RepositoryURL); e != nil {
		return e
	}
	if len(policy.RequiredChecks) == 0 || len(policy.RequiredChecks) > 32 {
		return Fail("invalid_input", "GitHub 模式需要 1–32 项明确的 CI 检查名称")
	}
	seen := map[string]bool{}
	for _, name := range policy.RequiredChecks {
		if strings.TrimSpace(name) != name || !ChatText(name, 256, true) || strings.ContainsAny(name, "\r\n") || name == ReviewCheckContext || seen[name] {
			return Fail("invalid_input", "GitHub CI 检查名称无效或重复")
		}
		seen[name] = true
	}
	return nil
}
