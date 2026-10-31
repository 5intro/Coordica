package github

import (
	"context"
	"errors"
	"strings"
	"time"

	"coordica/internal/domain"
	"coordica/internal/jsoninput"
)

const codexReviewMarker = "<!-- codex-review-state:v1 "

type providerComment struct {
	IssueURL  string    `json:"issue_url"`
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
	remaining := 64 << 10
	details := map[int64]string{}
	checks, err := c.checksWithDetails(ctx, s.Head, &remaining, details)
	if err != nil {
		return nil, err
	}
	out := []domain.ExternalReview{}
	for _, policy := range s.ReviewProviders {
		for _, kind := range policy.Kinds {
			report := domain.ExternalReview{Provider: policy.Provider, Kind: kind, ActorID: policy.ActorID, AppID: policy.AppID,
				Commit: s.Head, State: "pending", Summary: "Trusted review result missing on current commit"}
			check := reviewCheckSource(policy, kind, checks)
			if check.ID > 0 {
				report, err = c.linkedReview(ctx, s, report, details[check.ID])
				if err != nil {
					return nil, err
				}
			}
			out = append(out, bindReviewCheck(policy, kind, report, checks))
		}
	}
	return out, nil
}

func decodeCodexReports(body string) (map[string]*codexReviewReport, error) {
	if len(body) > 64<<10 || strings.Count(body, codexReviewMarker) != 1 {
		return nil, errors.New("ambiguous or oversized review envelope")
	}
	start := strings.Index(body, codexReviewMarker) + len(codexReviewMarker)
	end := strings.Index(body[start:], " -->")
	if end < 0 {
		return nil, errors.New("review envelope incomplete")
	}
	var reports map[string]*codexReviewReport
	if err := jsoninput.DecodeNullable([]byte(body[start:start+end]), &reports, 64<<10); err != nil {
		return nil, err
	}
	for _, v := range reports {
		if v != nil && !fullSHA.MatchString(v.Commit) {
			return nil, errors.New("review commit identity is invalid")
		}
	}
	return reports, nil
}

func codexReportState(v *codexReviewReport, err error) (string, string) {
	if err != nil {
		return "pending", "Trusted review report is malformed; cannot establish current approval"
	}
	if v == nil {
		return "pending", "Trusted review result is null; cannot establish current approval"
	}
	if v.Status != "completed" {
		return "pending", "Trusted review operation is incomplete or failed; no code repair inferred"
	}
	if v.Any == nil || v.Unresolved == nil || !*v.Any && *v.Unresolved {
		return "pending", "Trusted review result flags are missing or inconsistent; cannot establish current approval"
	}
	if *v.Unresolved {
		return "failure", "Trusted provider reports unresolved findings on the fixed commit"
	}
	return "success", "Trusted provider reports no unresolved findings on the fixed commit; not an Owner approval"
}
