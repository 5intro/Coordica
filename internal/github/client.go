// Package github translates fixed pull-request operations into GitHub's REST
// protocol. It owns no task state, scheduling, database or process execution.
package github

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"coordica/internal/domain"
)

type Client struct {
	repository string
	tokenFile  string
	baseURL    string
	http       *http.Client
}

var fullSHA = regexp.MustCompile(`^[a-f0-9]{40}$`)
var workBranch = regexp.MustCompile(`^coordica/task_[A-Za-z0-9_-]{1,128}$`)

func New(repository, tokenFile string) (*Client, error) {
	parsed, e := domain.GitHubRepository("https://github.com/" + repository + ".git")
	if e != nil || parsed != repository || !filepath.IsAbs(tokenFile) {
		return nil, errors.New("fixed GitHub repository and absolute token file required")
	}
	return &Client{repository: repository, tokenFile: tokenFile, baseURL: "https://api.github.com", http: &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("GitHub redirects are disabled") }}}, nil
}

func (c *Client) token() (string, error) {
	f, e := os.Open(c.tokenFile)
	if e != nil {
		return "", errors.New("GitHub credential file unavailable")
	}
	raw, e := io.ReadAll(io.LimitReader(f, 8193))
	f.Close()
	token := strings.TrimSpace(string(raw))
	if e != nil || len(raw) > 8192 || token == "" || strings.ContainsAny(token, "\x00\r\n\t ") {
		return "", errors.New("invalid GitHub credential file")
	}
	return token, nil
}

// HTTPSAuthorization lets the center reuse one private GitHub credential for
// Git transport, still bound by gitops to one exact HTTPS repository URL.
func (c *Client) HTTPSAuthorization(remote string) (string, error) {
	repository, e := domain.GitHubRepository(remote)
	if e != nil || repository != c.repository {
		return "", errors.New("Git credential repository mismatch")
	}
	token, e := c.token()
	if e != nil {
		return "", e
	}
	return "Basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:"+token)), nil
}

func (c *Client) request(ctx context.Context, method, path string, input, output any) error {
	token, e := c.token()
	if e != nil {
		return e
	}
	var body io.Reader
	if input != nil {
		b, e := json.Marshal(input)
		if e != nil {
			return e
		}
		body = bytes.NewReader(b)
	}
	req, e := http.NewRequestWithContext(ctx, method, c.baseURL+"/repos/"+c.repository+path, body)
	if e != nil {
		return e
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Content-Type", "application/json")
	resp, e := c.http.Do(req)
	if e != nil {
		return errors.New("GitHub request failed; remote state must be reconciled")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &apiError{Status: resp.StatusCode}
	}
	if output == nil {
		return nil
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, 4<<20+1))
	if e != nil || len(b) > 4<<20 {
		return errors.New("GitHub response exceeded limit or was interrupted")
	}
	if e = json.Unmarshal(b, output); e != nil {
		return errors.New("invalid GitHub response")
	}
	return nil
}

type apiError struct{ Status int }

func (e *apiError) Error() string { return fmt.Sprintf("GitHub HTTP %d", e.Status) }

type pull struct {
	Number         int        `json:"number"`
	State          string     `json:"state"`
	Draft          bool       `json:"draft"`
	Merged         bool       `json:"merged"`
	MergedAt       *time.Time `json:"merged_at"`
	MergeCommit    string     `json:"merge_commit_sha"`
	Mergeable      *bool      `json:"mergeable"`
	MergeableState string     `json:"mergeable_state"`
	Head           struct {
		SHA  string `json:"sha"`
		Ref  string `json:"ref"`
		Repo struct {
			FullName string `json:"full_name"`
		} `json:"repo"`
	} `json:"head"`
	Base struct {
		SHA  string `json:"sha"`
		Ref  string `json:"ref"`
		Repo struct {
			FullName string `json:"full_name"`
		} `json:"repo"`
	} `json:"base"`
}

func (c *Client) validate(spec domain.PullRequestSpec) error {
	if spec.Repository != c.repository || !workBranch.MatchString(spec.Branch) || spec.TargetBranch == "" || strings.ContainsAny(spec.TargetBranch, "\x00\r\n") || !fullSHA.MatchString(spec.Head) || !fullSHA.MatchString(spec.Base) {
		return errors.New("GitHub request outside fixed repository or candidate")
	}
	return nil
}
func (c *Client) getPull(ctx context.Context, s domain.PullRequestSpec) (pull, error) {
	var p pull
	e := c.request(ctx, "GET", "/pulls/"+strconv.Itoa(s.Number), nil, &p)
	if e == nil && (p.Number != s.Number || p.Head.Ref != s.Branch || p.Base.Ref != s.TargetBranch || p.Head.Repo.FullName != c.repository || p.Base.Repo.FullName != c.repository) {
		e = errors.New("GitHub PR identity changed")
	}
	return p, e
}

func (c *Client) Ensure(ctx context.Context, s domain.PullRequestSpec) (domain.PullRequestStatus, error) {
	if e := c.validate(s); e != nil {
		return domain.PullRequestStatus{}, e
	}
	if s.Number == 0 {
		owner := strings.Split(c.repository, "/")[0]
		query := "/pulls?state=all&per_page=100&head=" + url.QueryEscape(owner+":"+s.Branch) + "&base=" + url.QueryEscape(s.TargetBranch)
		var matches []pull
		if e := c.request(ctx, "GET", query, nil, &matches); e != nil {
			return domain.PullRequestStatus{}, e
		}
		if len(matches) > 1 {
			return domain.PullRequestStatus{}, errors.New("multiple PRs for one fixed work branch")
		}
		if len(matches) == 1 {
			s.Number = matches[0].Number
		} else {
			var p pull
			e := c.request(ctx, "POST", "/pulls", map[string]any{"title": s.Title, "head": s.Branch, "base": s.TargetBranch, "body": "Managed by Coordica. Tests and independent review are bound to the current candidate commit.", "maintainer_can_modify": false}, &p)
			if e != nil { // POST may have succeeded before a lost response; never blindly create another PR.
				if retry := c.request(ctx, "GET", query, nil, &matches); retry != nil || len(matches) != 1 {
					return domain.PullRequestStatus{}, e
				}
				s.Number = matches[0].Number
			} else {
				s.Number = p.Number
			}
		}
	}
	return c.Inspect(ctx, s)
}

func (c *Client) PublishReview(ctx context.Context, s domain.PullRequestSpec, state string) error {
	if e := c.validate(s); e != nil {
		return e
	}
	if state != "pending" && state != "success" && state != "failure" && state != "error" {
		return errors.New("invalid review check state")
	}
	return c.request(ctx, "POST", "/statuses/"+s.Head, map[string]string{"state": state, "context": domain.ReviewCheckContext, "description": "Coordica fixed candidate review"}, nil)
}

// Merge deliberately uses merge commits: the exact reviewed candidate must
// remain an ancestor. No squash/rebase, admin override or policy mutation.
func (c *Client) Merge(ctx context.Context, s domain.PullRequestSpec) (string, error) {
	facts, e := c.Inspect(ctx, s)
	if e != nil {
		return "", e
	}
	if facts.State == "merged" && facts.Head == s.Head {
		return facts.MergeCommit, nil
	}
	if !facts.MergeReady || facts.Head != s.Head || facts.Base != s.Base {
		return "", domain.ErrPublicationNotReady
	}
	var result struct {
		Merged bool   `json:"merged"`
		SHA    string `json:"sha"`
	}
	e = c.request(ctx, "PUT", "/pulls/"+strconv.Itoa(s.Number)+"/merge", map[string]string{"sha": s.Head, "merge_method": "merge"}, &result)
	if e != nil {
		var api *apiError
		if errors.As(e, &api) && (api.Status == 405 || api.Status == 409 || api.Status == 422) {
			return "", domain.ErrPublicationNotReady
		}
		return "", e
	}
	if !result.Merged || len(result.SHA) != 40 {
		return "", errors.New("GitHub did not confirm merge")
	}
	return result.SHA, nil
}
