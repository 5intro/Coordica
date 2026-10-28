package github

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"coordica/internal/domain"
	"coordica/internal/jsoninput"
)

const codexReviewMarker = "<!-- codex-review-state:v1 "

type providerComment struct {
	ID        int64     `json:"id"`
	Body      string    `json:"body"`
	UpdatedAt time.Time `json:"updated_at"`
	User      struct {
		ID int64 `json:"id"`
	} `json:"user"`
	App *struct {
		ID int64 `json:"id"`
	} `json:"performed_via_github_app"`
}

type codexReviewReport struct {
	Commit     string `json:"commit_sha"`
	Status     string `json:"status"`
	Any        *bool  `json:"has_any_findings"`
	Unresolved *bool  `json:"has_unresolved_findings"`
}

func (c *Client) externalReviews(ctx context.Context, s domain.PullRequestSpec) ([]domain.ExternalReview, error) {
	if len(s.ReviewProviders) == 0 {
		return nil, nil
	}
	var comments []providerComment
	for page := 1; page <= 10; page++ {
		var batch []providerComment
		if err := c.request(ctx, "GET", "/issues/"+strconv.Itoa(s.Number)+"/comments?per_page=100&page="+strconv.Itoa(page), nil, &batch); err != nil {
			return nil, err
		}
		comments = append(comments, batch...)
		if len(batch) < 100 {
			break
		}
		if page == 10 {
			return nil, errors.New("external review history exceeds complete synchronization bound")
		}
	}
	sort.SliceStable(comments, func(i, j int) bool {
		if comments[i].UpdatedAt.Equal(comments[j].UpdatedAt) {
			return comments[i].ID < comments[j].ID
		}
		return comments[i].UpdatedAt.Before(comments[j].UpdatedAt)
	})
	out := []domain.ExternalReview{}
	for _, policy := range s.ReviewProviders {
		current := map[string]domain.ExternalReview{}
		for _, kind := range policy.Kinds {
			current[kind] = domain.ExternalReview{Provider: policy.Provider, Kind: kind, ActorID: policy.ActorID, AppID: policy.AppID, Commit: s.Head, State: "pending", Summary: "Trusted review result missing on current commit"}
		}
		for _, comment := range comments {
			if comment.User.ID != policy.ActorID || comment.App == nil || comment.App.ID != policy.AppID || !strings.Contains(comment.Body, codexReviewMarker) {
				continue
			}
			reports, err := decodeCodexReports(comment.Body)
			if comment.ID <= 0 || comment.UpdatedAt.IsZero() {
				return nil, errors.New("invalid trusted review comment identity")
			}
			for _, kind := range policy.Kinds {
				report, exists := reports[kind]
				if err == nil && !exists {
					continue
				}
				if err == nil && report.Commit != s.Head {
					continue
				}
				v := current[kind]
				v.ReportID = comment.ID
				v.URL = "https://github.com/" + c.repository + "/pull/" + strconv.Itoa(s.Number) + "#issuecomment-" + strconv.FormatInt(comment.ID, 10)
				v.State, v.Summary = codexReportState(report, err)
				v.Evidence = ""
				if v.State == "failure" {
					remaining := 4096
					v.Evidence = "Provider comment excerpt (at most 4096 bytes; full report at source URL):\n" + boundedFeedback(comment.Body, &remaining)
				}
				current[kind] = v
			}
		}
		for _, kind := range policy.Kinds {
			out = append(out, current[kind])
		}
	}
	return out, nil
}

func decodeCodexReports(body string) (map[string]codexReviewReport, error) {
	if len(body) > 64<<10 || strings.Count(body, codexReviewMarker) != 1 {
		return nil, errors.New("ambiguous or oversized review envelope")
	}
	start := strings.Index(body, codexReviewMarker) + len(codexReviewMarker)
	end := strings.Index(body[start:], " -->")
	if end < 0 {
		return nil, errors.New("review envelope incomplete")
	}
	var reports map[string]codexReviewReport
	if err := jsoninput.Decode([]byte(body[start:start+end]), &reports, 64<<10); err != nil {
		return nil, err
	}
	for _, v := range reports {
		if !fullSHA.MatchString(v.Commit) || v.Any == nil || v.Unresolved == nil || !*v.Any && *v.Unresolved {
			return nil, errors.New("review identity or required result flags missing")
		}
	}
	return reports, nil
}

func codexReportState(v codexReviewReport, err error) (string, string) {
	if err != nil {
		return "pending", "Trusted review report is malformed; cannot establish current approval"
	}
	if v.Status != "completed" {
		return "pending", "Trusted review operation is incomplete or failed; no code repair inferred"
	}
	if *v.Unresolved {
		return "failure", "Trusted provider reports unresolved findings on the fixed commit"
	}
	return "success", "Trusted provider reports no unresolved findings on the fixed commit; not an Owner approval"
}
