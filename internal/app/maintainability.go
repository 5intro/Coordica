package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"coordica/internal/codequality"
	"coordica/internal/domain"
	"coordica/internal/gitops"
)

// This mandatory gate is owned by the center binary, not a candidate's scripts
// or allowlist. Both snapshots are fixed objects; no project code is executed.
func checkCandidateQuality(ctx context.Context, w *Integrator, c domain.Candidate) error {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	base, err := qualitySource(ctx, w.Git, w.repo(), c.Main)
	if err != nil {
		return err
	}
	next, err := qualitySource(ctx, w.Git, w.repo(), c.Commit)
	if err != nil {
		return err
	}
	findings, err := codequality.Regressions(base, next)
	if err != nil {
		return err
	}
	if len(findings) == 0 {
		return nil
	}
	var message strings.Builder
	fmt.Fprintf(&message, "%s: %d structural regressions; changes required", codequality.Version, len(findings))
	for _, finding := range findings[:min(8, len(findings))] {
		fmt.Fprintf(&message, "\n%s", finding)
	}
	return domain.Fail("maintainability", message.String())
}

func qualitySource(ctx context.Context, g *gitops.Client, repo, commit string) ([]codequality.File, error) {
	var files []codequality.File
	err := (gitops.SourceReader{Git: g}).Read(ctx, repo, gitops.OID(commit), codequality.Included, func(path, text string) error {
		files = append(files, codequality.File{Path: path, Text: text})
		return nil
	})
	return files, err
}
