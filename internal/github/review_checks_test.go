package github

import (
	"context"
	"coordica/internal/domain"
	"fmt"
	"strings"
	"testing"
)

func reviewCheckNames(kinds []string) map[string]string {
	names := map[string]string{}
	for _, kind := range kinds {
		names[kind] = map[string]string{"code_review": "Codex code", "security_review": "Codex security"}[kind]
	}
	return names
}

func fixtureReviewChecks(f *protocolFixture, states map[string]string) {
	commentID := 10
	for _, comment := range f.comments {
		commentID = max(commentID, comment["id"].(int))
	}
	f.checks = []map[string]any{{"id": 1, "name": "ci", "status": "completed", "conclusion": "success", "head_sha": f.spec.Head, "app": map[string]int{"id": 42}}}
	for _, policy := range f.spec.ReviewProviders {
		for i, kind := range policy.Kinds {
			state := states[kind]
			if state == "" {
				state = "success"
			}
			status := "completed"
			if state == "pending" {
				status = "in_progress"
			}
			f.checks = append(f.checks, map[string]any{"id": 20 + i, "name": policy.CheckNames[kind], "status": status,
				"conclusion": state, "head_sha": f.spec.Head, "app": map[string]int64{"id": policy.AppID},
				"details_url": fmt.Sprintf("https://github.com/owner/repo/pull/7#issuecomment-%d", commentID)})
		}
	}
}

func TestExternalReviewRequiresPinnedAppCheckAndConsistentVerdict(t *testing.T) {
	for _, mode := range []string{"missing", "legacy status", "wrong app", "unpinned branch", "wrong head", "operation failure", "contradictory verdict"} {
		t.Run(mode, func(t *testing.T) {
			f := protocol(t)
			f.spec.Number = 7
			f.spec.ReviewProviders = []domain.ExternalReviewPolicy{{Provider: "codex", ActorID: 42, AppID: 99, Kinds: []string{"security_review"}, CheckNames: reviewCheckNames([]string{"security_review"})}}
			f.comments = []map[string]any{externalComment(10, f.spec.Head, "completed", false)}
			fixtureReviewChecks(f, nil)
			switch mode {
			case "missing":
				f.checks = f.checks[:1]
			case "legacy status":
				f.checks = f.checks[:1]
				f.extraStatuses = []map[string]any{{"id": 100, "context": "Codex security", "state": "success"}}
			case "wrong app":
				f.checks[1]["app"] = map[string]int{"id": 100}
			case "unpinned branch":
				f.externalUnpinned = true
			case "wrong head":
				f.checks[1]["head_sha"] = strings.Repeat("e", 40)
			case "operation failure":
				f.comments = []map[string]any{externalComment(10, f.spec.Head, "failed", false)}
				f.checks[1]["conclusion"] = "failure"
			case "contradictory verdict":
				f.comments = []map[string]any{externalComment(10, f.spec.Head, "completed", true)}
			}
			facts, err := f.client.Inspect(context.Background(), f.spec)
			if facts.MergeReady || mode != "wrong head" && err != nil {
				t.Fatal("untrusted/inconsistent check passed", mode, facts, err)
			}
			if (mode == "operation failure" || mode == "contradictory verdict") && (facts.CI == "failure" || facts.Review == "changes_requested") {
				t.Fatal("provider operation/configuration mismatch inferred a code repair", facts)
			}
			if _, err := f.client.Merge(context.Background(), f.spec); err == nil || f.merge != 0 {
				t.Fatal("invalid App check reached merge", err)
			}
		})
	}
}
