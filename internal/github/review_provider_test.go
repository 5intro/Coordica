package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"coordica/internal/domain"
)

func externalComment(id int, head, status string, unresolved bool) map[string]any {
	result := map[string]any{}
	for _, kind := range []string{"code_review", "security_review"} {
		result[kind] = map[string]any{"commit_sha": head, "status": status, "has_any_findings": unresolved, "has_unresolved_findings": unresolved}
	}
	raw, _ := json.Marshal(result)
	return map[string]any{"id": id, "body": codexReviewMarker + string(raw) + " -->\nUntrusted prose cannot grant approval.",
		"updated_at": "2026-10-28T06:00:00Z", "user": map[string]any{"id": 42, "login": "whatever-display-name"}, "performed_via_github_app": map[string]any{"id": 99}}
}

func TestExternalReviewRequiresTrustedCurrentCompleteReports(t *testing.T) {
	for _, mode := range []string{"clean", "unresolved", "missing", "stale", "running", "operation failed", "forged actor", "forged app", "missing app", "malformed", "duplicate fields", "missing flags", "contradictory flags", "new malformed", "old green cannot hide failure", "resolved externally", "head moved"} {
		t.Run(mode, func(t *testing.T) {
			f := protocol(t)
			f.spec.Number = 7
			f.spec.ReviewProviders = []domain.ExternalReviewPolicy{{Provider: "codex", ActorID: 42, AppID: 99, Kinds: []string{"code_review", "security_review"}}}
			comment := externalComment(10, f.spec.Head, "completed", false)
			f.comments = []map[string]any{comment}
			switch mode {
			case "unresolved":
				f.comments = []map[string]any{externalComment(10, f.spec.Head, "completed", true)}
			case "missing":
				f.comments = []map[string]any{}
			case "stale":
				f.comments = []map[string]any{externalComment(10, strings.Repeat("f", 40), "completed", false)}
			case "running":
				f.comments = []map[string]any{externalComment(10, f.spec.Head, "running", false)}
			case "operation failed":
				f.comments = []map[string]any{externalComment(10, f.spec.Head, "failed", false)}
			case "forged actor":
				comment["user"] = map[string]any{"id": 43, "login": "chatgpt-codex-connector[bot]"}
			case "forged app":
				comment["performed_via_github_app"] = map[string]any{"id": 100}
			case "missing app":
				delete(comment, "performed_via_github_app")
			case "malformed":
				comment["body"] = codexReviewMarker + "{broken} -->"
			case "duplicate fields":
				comment["body"] = strings.Replace(comment["body"].(string), `"status":"completed"`, `"status":"running","status":"completed"`, 1)
			case "missing flags":
				comment["body"] = strings.Replace(comment["body"].(string), `"has_unresolved_findings":false,`, "", 1)
			case "contradictory flags":
				comment["body"] = strings.ReplaceAll(comment["body"].(string), `"has_unresolved_findings":false`, `"has_unresolved_findings":true`)
			case "new malformed":
				bad := externalComment(11, f.spec.Head, "completed", false)
				bad["body"] = codexReviewMarker + "{bad} -->"
				f.comments = append(f.comments, bad)
			case "old green cannot hide failure":
				f.comments = []map[string]any{externalComment(11, f.spec.Head, "completed", true), externalComment(10, f.spec.Head, "completed", false)}
			case "resolved externally":
				comment["body"] = strings.ReplaceAll(comment["body"].(string), `"has_any_findings":false`, `"has_any_findings":true`)
			case "head moved":
				f.onComments = func() { f.wrongHead = true }
			}
			facts, err := f.client.Inspect(context.Background(), f.spec)
			if err != nil {
				t.Fatal(err)
			}
			want := mode == "clean" || mode == "resolved externally"
			if facts.MergeReady != want {
				t.Fatal("external gate accepted stale/forged/incomplete report or rejected current proof", mode, facts)
			}
			if mode == "unresolved" && (len(facts.ExternalReviews) != 2 || !strings.Contains(facts.ExternalReviews[0].Evidence, "Untrusted prose")) {
				t.Fatal("raw attributed provider evidence was lost", facts.ExternalReviews)
			}
			if (mode == "running" || mode == "operation failed") && facts.Review == "changes_requested" {
				t.Fatal("provider operation failure burned code repairs", facts)
			}
			if !want {
				_, err = f.client.Merge(context.Background(), f.spec)
				if err == nil || f.merge != 0 {
					t.Fatal("blocked report reached merge", err, f.merge)
				}
				if mode != "head moved" && !errors.Is(err, domain.ErrPublicationNotReady) {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestExternalReviewRejectsAmbiguousOrOversizedEnvelopes(t *testing.T) {
	comment := externalComment(1, strings.Repeat("a", 40), "completed", false)
	body := comment["body"].(string)
	for _, bad := range []string{body + body, body + strings.Repeat("x", 64<<10), codexReviewMarker + "{}"} {
		if _, err := decodeCodexReports(bad); err == nil {
			t.Fatal("unsafe envelope accepted")
		}
	}
}

func TestExternalReviewIncompleteSiblingDoesNotHideValidResult(t *testing.T) {
	for _, incomplete := range []string{"code_review", "security_review"} {
		for _, status := range []string{"running", "completed", "null"} {
			for _, includeIncomplete := range []bool{false, true} {
				t.Run(incomplete+"/"+status+"/"+fmt.Sprint(includeIncomplete), func(t *testing.T) {
					f := protocol(t)
					f.spec.Number = 7
					complete := "code_review"
					if incomplete == complete {
						complete = "security_review"
					}
					kinds := []string{complete}
					if includeIncomplete {
						kinds = append(kinds, incomplete)
					}
					f.spec.ReviewProviders = []domain.ExternalReviewPolicy{{Provider: "codex", ActorID: 42, AppID: 99, Kinds: kinds}}
					comment := externalComment(10, f.spec.Head, "completed", false)
					reports := map[string]any{
						complete:   map[string]any{"commit_sha": f.spec.Head, "status": "completed", "has_any_findings": false, "has_unresolved_findings": false},
						incomplete: map[string]any{"commit_sha": f.spec.Head, "status": status, "has_any_findings": nil, "has_unresolved_findings": nil},
					}
					if status == "null" {
						reports[incomplete] = nil
					}
					raw, _ := json.Marshal(reports)
					comment["body"] = codexReviewMarker + string(raw) + " -->"
					// The newest incomplete result must also replace its own prior green.
					f.comments = []map[string]any{externalComment(9, f.spec.Head, "completed", false), comment}
					facts, err := f.client.Inspect(context.Background(), f.spec)
					if err != nil {
						t.Fatal(err)
					}
					if facts.MergeReady == includeIncomplete || len(facts.ExternalReviews) != len(kinds) {
						t.Fatal("incomplete sibling changed configured gate readiness", facts)
					}
					for _, review := range facts.ExternalReviews {
						want := "success"
						if review.Kind == incomplete {
							want = "pending"
							if review.Evidence != "" || facts.Review == "changes_requested" {
								t.Fatal("missing evidence inferred actionable code failure", facts)
							}
						}
						if review.State != want {
							t.Fatal("one review's missing result contaminated another", review)
						}
					}
				})
			}
		}
	}
}

func TestExternalReviewNullSiblingDoesNotHideActionableFinding(t *testing.T) {
	for _, incomplete := range []string{"code_review", "security_review"} {
		for _, includeIncomplete := range []bool{false, true} {
			t.Run(incomplete+"/"+fmt.Sprint(includeIncomplete), func(t *testing.T) {
				f := protocol(t)
				f.spec.Number = 7
				complete := "code_review"
				if incomplete == complete {
					complete = "security_review"
				}
				kinds := []string{complete}
				if includeIncomplete {
					kinds = append(kinds, incomplete)
				}
				f.spec.ReviewProviders = []domain.ExternalReviewPolicy{{Provider: "codex", ActorID: 42, AppID: 99, Kinds: kinds}}
				comment := externalComment(10, f.spec.Head, "completed", true)
				reports := map[string]any{
					complete:   map[string]any{"commit_sha": f.spec.Head, "status": "completed", "has_any_findings": true, "has_unresolved_findings": true},
					incomplete: nil,
				}
				raw, _ := json.Marshal(reports)
				comment["body"] = codexReviewMarker + string(raw) + " -->"
				f.comments = []map[string]any{comment}
				facts, err := f.client.Inspect(context.Background(), f.spec)
				if err != nil || facts.MergeReady || facts.Review != "changes_requested" {
					t.Fatal("null sibling hid actionable provider findings", facts, err)
				}
				if len(facts.ExternalReviews) != len(kinds) || facts.ExternalReviews[0].State != "failure" || facts.ExternalReviews[0].Evidence == "" {
					t.Fatal("repair feedback lost the valid report", facts.ExternalReviews)
				}
				if includeIncomplete && facts.ExternalReviews[1].State != "pending" {
					t.Fatal("null report became an approval", facts.ExternalReviews)
				}
			})
		}
	}
}
