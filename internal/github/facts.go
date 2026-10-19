package github

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"unicode/utf8"

	"coordica/internal/domain"
)

type protectedCheck struct {
	Context string `json:"context"`
	AppID   *int64 `json:"app_id"`
}
type protection struct {
	EnforceAdmins struct {
		Enabled bool `json:"enabled"`
	} `json:"enforce_admins"`
	RequiredChecks *struct {
		Strict   bool             `json:"strict"`
		Checks   []protectedCheck `json:"checks"`
		Contexts []string         `json:"contexts"`
	} `json:"required_status_checks"`
	LinearHistory struct {
		Enabled bool `json:"enabled"`
	} `json:"required_linear_history"`
}

func (c *Client) Inspect(ctx context.Context, s domain.PullRequestSpec) (domain.PullRequestStatus, error) {
	var out domain.PullRequestStatus
	if e := c.validate(s); e != nil {
		return out, e
	}
	p, e := c.getPull(ctx, s)
	if e != nil {
		return out, e
	}
	out = domain.PullRequestStatus{Number: p.Number, URL: "https://github.com/" + c.repository + "/pull/" + strconv.Itoa(p.Number), Branch: p.Head.Ref, Head: p.Head.SHA, Base: p.Base.SHA, State: p.State, CI: "pending", Review: "pending", Checks: []domain.RemoteCheck{}, Reviews: []domain.RemoteReview{}}
	if p.Merged || p.MergedAt != nil {
		out.State = "merged"
		out.MergeCommit = p.MergeCommit
		return out, nil
	}
	if p.State != "open" {
		out.Detail = "PR 已关闭；不会重新创建或自动重开"
		return out, nil
	}
	if p.Head.SHA != s.Head || p.Base.SHA != s.Base {
		out.Detail = "PR 提交或主线已变化"
		return out, nil
	}
	feedbackBytes := 64 << 10
	checks, e := c.checks(ctx, s.Head, &feedbackBytes)
	if e != nil {
		return out, e
	}
	out.Checks = checks
	out.Reviews, e = c.reviews(ctx, s, &feedbackBytes)
	if e != nil {
		return out, e
	}
	out.Review = "none"
	for _, r := range out.Reviews {
		if r.State == "CHANGES_REQUESTED" {
			out.Review = "changes_requested"
			break
		}
		if r.State == "APPROVED" && r.Commit == s.Head {
			out.Review = "approved"
		}
	}
	var rules protection
	e = c.request(ctx, "GET", "/branches/"+url.PathEscape(s.TargetBranch)+"/protection", nil, &rules)
	if e != nil {
		var api *apiError
		if errors.As(e, &api) && (api.Status == 404 || api.Status == 403) {
			out.Detail = "需启用可验证的 GitHub 分支保护"
			return out, nil
		}
		return out, e
	}
	if rules.RequiredChecks == nil || !rules.RequiredChecks.Strict || !rules.EnforceAdmins.Enabled || rules.LinearHistory.Enabled {
		out.Detail = "分支需强制必需检查、保持最新并约束管理员；合并方式需允许 merge commit"
		return out, nil
	}
	required := map[string]*int64{}
	for _, check := range rules.RequiredChecks.Checks {
		required[check.Context] = check.AppID
	}
	for _, name := range rules.RequiredChecks.Contexts {
		if _, ok := required[name]; !ok {
			required[name] = nil
		}
	}
	for _, name := range append(append([]string{}, s.RequiredChecks...), domain.ReviewCheckContext) {
		if _, ok := required[name]; !ok {
			out.Detail = "分支保护缺少必需检查：" + name
			return out, nil
		}
	}
	out.CI = "success"
	for name, appID := range required {
		matched := []domain.RemoteCheck{}
		for _, check := range checks {
			if check.Name == name && (appID == nil || *appID < 0 || check.AppID == *appID) {
				matched = append(matched, check)
			}
		}
		if len(matched) != 1 {
			out.CI = "pending"
			out.Detail = "检查缺失或来源不唯一：" + name
			if len(matched) == 0 && appID != nil && *appID > 0 {
				for _, check := range checks {
					if check.Name == name && check.AppID == 0 {
						// REST commit statuses expose a creator, not a GitHub App
						// identity. Never infer the pinned source from that creator.
						out.Detail = "无法核验绑定 GitHub App 的 legacy commit status：" + name + "；请使用带 App 身份的 Check Run 或由 Owner 调整来源策略"
						return out, nil
					}
				}
			}
			continue
		}
		if matched[0].State == "failure" {
			out.CI = "failure"
			out.Detail = "必需检查失败：" + name
			break
		}
		if matched[0].State != "success" {
			out.CI = "pending"
			out.Detail = "等待必需检查：" + name
		}
	}
	if out.Review == "changes_requested" {
		out.Detail = "GitHub 评审要求修改"
	}
	out.MergeReady = out.CI == "success" && out.Review != "changes_requested" && !p.Draft && p.Mergeable != nil && *p.Mergeable && (p.MergeableState == "clean" || p.MergeableState == "has_hooks" || p.MergeableState == "unstable")
	if !out.MergeReady && out.Detail == "" {
		out.Detail = "等待 GitHub 合并条件；不追加 Coordica 人工审批"
	}
	return out, nil
}

func (c *Client) checks(ctx context.Context, head string, feedbackBytes *int) ([]domain.RemoteCheck, error) {
	checks := []domain.RemoteCheck{}
	// Keep the latest run for each source/name. A different source with the same
	// name remains distinct, so an unpinned required check cannot be spoofed.
	indexes := map[string]int{}
	latestIDs := map[string]int64{}
	for page := 1; page <= 10; page++ {
		var data struct {
			Total int `json:"total_count"`
			Runs  []struct {
				ID         int64  `json:"id"`
				Name       string `json:"name"`
				Status     string `json:"status"`
				Conclusion string `json:"conclusion"`
				Output     struct {
					Summary string `json:"summary"`
				} `json:"output"`
				Head string `json:"head_sha"`
				App  struct {
					ID int64 `json:"id"`
				} `json:"app"`
			} `json:"check_runs"`
		}
		if e := c.request(ctx, "GET", "/commits/"+head+"/check-runs?filter=latest&per_page=100&page="+strconv.Itoa(page), nil, &data); e != nil {
			return nil, e
		}
		if data.Total > 1000 {
			return nil, errors.New("GitHub check history exceeds synchronization bound")
		}
		for _, r := range data.Runs {
			if r.Head != head || r.Name == "" || len(r.Name) > 256 || r.ID <= 0 || r.App.ID <= 0 {
				return nil, errors.New("invalid GitHub check identity")
			}
			key := strconv.FormatInt(r.App.ID, 10) + "/" + r.Name
			if latestIDs[key] >= r.ID {
				continue
			}
			latestIDs[key] = r.ID
			state := "pending"
			if r.Status == "completed" {
				if r.Conclusion == "success" {
					state = "success"
				} else if r.Conclusion == "failure" || r.Conclusion == "timed_out" {
					state = "failure"
				}
			}
			check := domain.RemoteCheck{Name: r.Name, AppID: r.App.ID, State: state, URL: "https://github.com/" + c.repository + "/commit/" + head + "/checks"}
			if state == "failure" {
				check.Summary = boundedFeedback(r.Output.Summary, feedbackBytes)
			}
			if index, ok := indexes[key]; ok {
				checks[index] = check
			} else {
				indexes[key] = len(checks)
				checks = append(checks, check)
			}
		}
		if len(data.Runs) < 100 {
			break
		}
		if page == 10 {
			return nil, errors.New("GitHub check pagination incomplete")
		}
	}
	indexes = map[string]int{}
	latestIDs = map[string]int64{}
	for page := 1; page <= 10; page++ {
		var statuses []struct {
			ID      int64  `json:"id"`
			Context string `json:"context"`
			State   string `json:"state"`
			SHA     string `json:"sha"`
		}
		if e := c.request(ctx, "GET", "/commits/"+head+"/statuses?per_page=100&page="+strconv.Itoa(page), nil, &statuses); e != nil {
			return nil, e
		}
		for _, s := range statuses {
			if s.Context == "" || len(s.Context) > 256 || s.ID <= 0 {
				return nil, errors.New("invalid GitHub status identity")
			}
			if latestIDs[s.Context] >= s.ID {
				continue
			}
			latestIDs[s.Context] = s.ID
			state := "pending"
			if s.State == "success" {
				state = "success"
			}
			if s.State == "error" || s.State == "failure" {
				state = "failure"
			}
			check := domain.RemoteCheck{Name: s.Context, State: state, URL: "https://github.com/" + c.repository + "/commit/" + head}
			if index, ok := indexes[s.Context]; ok {
				checks[index] = check
			} else {
				indexes[s.Context] = len(checks)
				checks = append(checks, check)
			}
		}
		if len(statuses) < 100 {
			break
		}
		if page == 10 {
			return nil, errors.New("GitHub status pagination incomplete")
		}
	}
	return checks, nil
}

func (c *Client) reviews(ctx context.Context, s domain.PullRequestSpec, feedbackBytes *int) ([]domain.RemoteReview, error) {
	latest := map[string]domain.RemoteReview{}
	order := []string{}
	for page := 1; page <= 10; page++ {
		var values []struct {
			ID     int64  `json:"id"`
			State  string `json:"state"`
			Commit string `json:"commit_id"`
			Body   string `json:"body"`
			User   struct {
				Login string `json:"login"`
			} `json:"user"`
		}
		if e := c.request(ctx, "GET", "/pulls/"+strconv.Itoa(s.Number)+"/reviews?per_page=100&page="+strconv.Itoa(page), nil, &values); e != nil {
			return nil, e
		}
		for _, r := range values {
			if r.User.Login == "" || len(r.User.Login) > 100 || r.ID <= 0 {
				return nil, errors.New("invalid GitHub reviewer identity")
			}
			// An unsubmitted draft cannot supersede a submitted review.
			if r.State == "PENDING" {
				continue
			}
			// Comments do not dismiss a prior request for changes or grant approval.
			if prior, ok := latest[r.User.Login]; ok && (r.ID <= prior.ID || r.State == "COMMENTED" && prior.State != "COMMENTED") {
				continue
			}
			if _, ok := latest[r.User.Login]; !ok {
				order = append(order, r.User.Login)
			}
			latest[r.User.Login] = domain.RemoteReview{ID: r.ID, Author: r.User.Login, State: r.State, Commit: r.Commit, URL: "https://github.com/" + c.repository + "/pull/" + strconv.Itoa(s.Number) + "#pullrequestreview-" + strconv.FormatInt(r.ID, 10), Summary: boundedFeedback(r.Body, feedbackBytes)}
		}
		if len(values) < 100 {
			break
		}
		if page == 10 {
			return nil, errors.New("GitHub review pagination incomplete")
		}
	}
	out := []domain.RemoteReview{}
	for _, name := range order {
		out = append(out, latest[name])
	}
	return out, nil
}

func boundedFeedback(text string, remaining *int) string {
	limit := min(4096, *remaining)
	if len(text) > limit {
		text = text[:limit]
		for len(text) > 0 && !utf8.ValidString(text) {
			text = text[:len(text)-1]
		}
	}
	*remaining -= len(text)
	return text
}
