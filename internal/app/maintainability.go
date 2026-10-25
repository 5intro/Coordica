package app

import (
	"context"
	"errors"
	"time"

	"coordica/internal/codequality"
	"coordica/internal/domain"
	"coordica/internal/gitops"
)

// Operational scan faults do not consume a repair allowance. Deterministic
// candidate inventory defects still enter bounded repair; metric signals do not.
func candidateQualityFailure(ctx context.Context, w *Integrator, c domain.Candidate, err error) error {
	var finding *domain.Error
	if errors.As(err, &finding) && finding.Code == "maintainability" {
		return w.reject(ctx, c, err, true)
	}
	return err
}

// Read fixed objects without executing project code. Source integrity stays
// mandatory; maintainability measurements are bounded review hints.
func checkCandidateQuality(ctx context.Context, w *Integrator, c *domain.Candidate) error {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	base, err := qualitySource(ctx, w.Git, w.repo(), c.Main)
	if err != nil {
		return err
	}
	next, err := qualitySource(ctx, w.Git, w.repo(), c.Commit)
	if err != nil {
		var inventory *gitops.SourceInventoryError
		if errors.As(err, &inventory) {
			return domain.Fail("maintainability", err.Error())
		}
		return err
	}
	findings, err := codequality.RegressionsContext(ctx, base, next)
	if err != nil {
		var candidate *codequality.CandidateError
		if errors.As(err, &candidate) {
			return domain.Fail("maintainability", err.Error())
		}
		return err
	}
	c.Maintainability = codequality.Summary(findings)
	return nil
}

func qualitySource(ctx context.Context, g *gitops.Client, repo, commit string) ([]codequality.File, error) {
	var files []codequality.File
	err := (gitops.SourceReader{Git: g}).Read(ctx, repo, gitops.OID(commit), codequality.Included, func(path, text string) error {
		files = append(files, codequality.File{Path: path, Text: text})
		return nil
	})
	return files, err
}
