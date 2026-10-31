package github

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"coordica/internal/domain"
)

func TestExternalReviewUnrelatedCommentHistoryCannotBlockProviderEvidence(t *testing.T) {
	for _, count := range []int{1001, 70} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			f := protocol(t)
			f.spec.Number = 7
			f.spec.ReviewProviders = []domain.ExternalReviewPolicy{{Provider: "codex", ActorID: 42, AppID: 99,
				Kinds: []string{"security_review"}, CheckNames: reviewCheckNames([]string{"security_review"})}}
			f.comments = []map[string]any{externalComment(10, f.spec.Head, "completed", false)}
			fixtureReviewChecks(f, nil) // The App-owned check points at its own report, before unrelated traffic.
			body := "unrelated"
			if count == 70 {
				body = strings.Repeat("x", 60000) // Legal-sized bodies exceed 4 MiB in one listing page.
			}
			for i := 0; i < count; i++ {
				f.comments = append(f.comments, map[string]any{"id": i + 100, "body": body, "user": map[string]int{"id": 300}})
			}
			facts, err := f.client.Inspect(context.Background(), f.spec)
			if err != nil || !facts.MergeReady || len(facts.ExternalReviews) != 1 || facts.ExternalReviews[0].ReportID != 10 {
				t.Fatal("unrelated public comments poisoned trusted evidence", facts, err)
			}
			if f.commentListings != 0 || len(f.commentReads) != 1 || f.commentReads[0] != 10 {
				t.Fatal("provider evidence scanned unrelated history", f.commentListings, f.commentReads)
			}
		})
	}
}

func TestExternalReviewAppReportReferenceIsBoundToCurrentPRAndIdentity(t *testing.T) {
	for _, mode := range []string{"missing", "foreign host", "foreign repository", "foreign PR", "query", "credentials", "encoded", "noncanonical ID", "deleted", "wrong issue metadata", "wrong ID metadata", "repository casing"} {
		t.Run(mode, func(t *testing.T) {
			f := protocol(t)
			f.spec.Number = 7
			f.spec.ReviewProviders = []domain.ExternalReviewPolicy{{Provider: "codex", ActorID: 42, AppID: 99,
				Kinds: []string{"security_review"}, CheckNames: reviewCheckNames([]string{"security_review"})}}
			comment := externalComment(10, f.spec.Head, "completed", false)
			f.comments = []map[string]any{comment}
			fixtureReviewChecks(f, nil)
			link := f.checks[1]["details_url"].(string)
			switch mode {
			case "missing":
				link = ""
			case "foreign host":
				link = strings.Replace(link, "github.com", "example.com", 1)
			case "foreign repository":
				link = strings.Replace(link, "owner/repo", "other/repo", 1)
			case "foreign PR":
				link = strings.Replace(link, "/pull/7", "/pull/8", 1)
			case "query":
				link = strings.Replace(link, "#", "?extra=1#", 1)
			case "credentials":
				link = strings.Replace(link, "https://", "https://user@", 1)
			case "encoded":
				link = strings.Replace(link, "comment-10", "comment-%31%30", 1)
			case "noncanonical ID":
				link = strings.Replace(link, "comment-10", "comment-010", 1)
			case "deleted":
				link = strings.Replace(link, "comment-10", "comment-11", 1)
			case "wrong issue metadata":
				comment["issue_url"] = "https://api.github.com/repos/owner/repo/issues/8"
			case "wrong ID metadata":
				f.onComments = func() { comment["id"] = 11 }
			case "repository casing":
				link = strings.Replace(link, "owner/repo", "OWNER/REPO", 1)
				comment["issue_url"] = "https://api.github.com/repos/OWNER/REPO/issues/7"
			}
			f.checks[1]["details_url"] = link
			facts, err := f.client.Inspect(context.Background(), f.spec)
			want := mode == "repository casing"
			if err != nil || facts.MergeReady != want || f.commentListings != 0 {
				t.Fatal("report reference escaped its fixed binding", mode, facts, err)
			}
			if !want && (facts.ExternalReviews[0].State != "pending" || facts.Review == "changes_requested") {
				t.Fatal("invalid reference granted approval or consumed code repairs", facts)
			}
		})
	}
}
