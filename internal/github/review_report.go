package github

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"

	"coordica/internal/domain"
)

// Only App-owned check metadata can select a report. Public comment traffic is
// never queried, and an arbitrary details URL is never used as a request URL.
func (c *Client) linkedReview(ctx context.Context, spec domain.PullRequestSpec, report domain.ExternalReview, link string) (domain.ExternalReview, error) {
	id := reviewCommentID(spec, link)
	if id == 0 {
		report.Summary = "App review check must link directly to its structured report on this PR"
		return report, nil
	}
	var comment providerComment
	if err := c.request(ctx, "GET", "/issues/comments/"+strconv.FormatInt(id, 10), nil, &comment); err != nil {
		var api *apiError
		if errors.As(err, &api) && api.Status == 404 {
			report.Summary = "The App-linked report is unavailable; fresh trusted evidence is required"
			return report, nil
		}
		return report, err
	}
	issueURL := "https://api.github.com/repos/" + c.repository + "/issues/" + strconv.Itoa(spec.Number)
	if comment.ID != id || comment.User.ID != report.ActorID || comment.App == nil || comment.App.ID != report.AppID ||
		comment.UpdatedAt.IsZero() || !strings.EqualFold(comment.IssueURL, issueURL) {
		report.Summary = "App-linked report identity, PR binding or version is invalid"
		return report, nil
	}
	report.ReportID, report.ReportUpdatedAt = id, comment.UpdatedAt
	report.URL = "https://github.com/" + c.repository + "/pull/" + strconv.Itoa(spec.Number) + "#issuecomment-" + strconv.FormatInt(id, 10)
	reports, err := decodeCodexReports(comment.Body)
	result := reports[report.Kind]
	if err == nil && result != nil && result.Commit != spec.Head {
		report.Summary = "App-linked report describes a different commit"
		return report, nil
	}
	report.State, report.Summary = codexReportState(result, err)
	if report.State == "failure" {
		remaining := 4096
		report.Evidence = "Provider comment excerpt (at most 4096 bytes; full report at source URL):\n" + boundedFeedback(comment.Body, &remaining)
	}
	return report, nil
}

func reviewCommentID(spec domain.PullRequestSpec, link string) int64 {
	u, err := url.Parse(link)
	if err != nil || u.Scheme != "https" || !strings.EqualFold(u.Host, "github.com") || u.User != nil ||
		u.RawQuery != "" || u.RawPath != "" || u.RawFragment != "" {
		return 0
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) != 4 || !strings.EqualFold(parts[0]+"/"+parts[1], spec.Repository) || parts[2] != "pull" || parts[3] != strconv.Itoa(spec.Number) {
		return 0
	}
	text := strings.TrimPrefix(u.Fragment, "issuecomment-")
	id, err := strconv.ParseInt(text, 10, 64)
	if err != nil || id <= 0 || u.Fragment != "issuecomment-"+strconv.FormatInt(id, 10) {
		return 0
	}
	return id
}
