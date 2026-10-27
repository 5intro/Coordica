package app

import (
	"context"
	"testing"

	"coordica/internal/domain"
)

func TestInitializationRootFileNeedsExplicitConfirmedRepositoryRoot(t *testing.T) {
	for _, root := range []string{"src", "."} {
		t.Run(root, func(t *testing.T) {
			s, owner, _ := initFixture(t)
			requestInit(t, s, owner, "existing")
			result := initResult(t, s, true)
			result.Baseline.AllowedRoots = []string{root}
			initialization := analyzeInit(t, s, owner, result)
			ctx := context.Background()
			raw, err := s.CreateTask(ctx, owner, "before-confirm", domain.Task{
				Title: "Root entrypoint", Description: "Create the authorized project entrypoint",
				Acceptance: "Build succeeds", Tool: "codex", AllowedPaths: []string{"index.html"},
			})
			if err == nil {
				t.Fatal("a proposed root granted task authority before Owner confirmation", string(raw))
			}
			_, err = s.ConfirmInitialization(ctx, owner, "confirm", domain.InitializationConfirm{
				ExpectedVersion: initialization.Version, Digest: initialization.Digest,
			})
			if err != nil {
				t.Fatal(err)
			}
			raw, err = s.CreateTask(ctx, owner, "root-task", domain.Task{
				Title: "Root entrypoint", Description: "Create the authorized project entrypoint",
				Acceptance: "Build succeeds", Tool: "codex", AllowedPaths: []string{"index.html"},
			})
			task := decodeTest[domain.Task](t, raw, err)
			_, err = s.TaskAction(ctx, owner, "approve", task.ID, "approve", domain.VersionRequest{
				ExpectedVersion: task.Version,
			})
			if root == "." && err != nil {
				t.Fatal("confirmed repository root rejected an ordinary root entrypoint", err)
			}
			if root == "src" && err == nil {
				t.Fatal("subdirectory scope silently authorized a root entrypoint")
			}
		})
	}
}
