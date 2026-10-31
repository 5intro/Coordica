package app

import (
	"context"
	"errors"
	"path"
	"sort"

	"coordica/internal/domain"
	"coordica/internal/gitops"
)

type reviewRuleFailure struct{ error }

func (e *reviewRuleFailure) Unwrap() error { return e.error }

func pinnedReviewRules(ctx context.Context, g *gitops.Client, repo string, c domain.Candidate, changes []gitops.Change) (*domain.ReviewRules, error) {
	dirs := map[string]bool{".": true}
	changed := map[string]bool{}
	for _, change := range changes {
		for _, file := range change.Paths() {
			if domain.ReviewRulePath(file) {
				changed[file] = true
			}
			for dir := path.Dir(file); dir != "."; dir = path.Dir(dir) {
				dirs[dir] = true
			}
		}
	}
	rules := &domain.ReviewRules{Version: domain.ReviewContractVersion, Base: c.Main, Files: []domain.ReviewRuleFile{}, Changed: []string{}}
	size := 0
	err := (gitops.SourceReader{Git: g}).Read(ctx, repo, gitops.OID(c.Main), func(file string) bool {
		return domain.ReviewRulePath(file) && dirs[path.Dir(file)]
	}, func(file, text string) error {
		size += len(text)
		if size > 64<<10 || len(rules.Files) >= 64 {
			return domain.Fail("context_limit", "complete trusted review rules exceed material bounds")
		}
		rules.Files = append(rules.Files, domain.ReviewRuleFile{Path: file, Text: text})
		return nil
	})
	if err != nil {
		var inventory *gitops.SourceInventoryError
		var input *domain.Error
		if errors.As(err, &inventory) || errors.As(err, &input) {
			return nil, &reviewRuleFailure{err}
		}
		return nil, err
	}
	sort.Slice(rules.Files, func(i, j int) bool { return rules.Files[i].Path < rules.Files[j].Path })
	for file := range changed {
		rules.Changed = append(rules.Changed, file)
	}
	sort.Strings(rules.Changed)
	rules.Digest = domain.ReviewRulesDigest(*rules)
	if err := domain.ValidateReviewRules(*rules, c.Main); err != nil {
		return nil, &reviewRuleFailure{err}
	}
	return rules, nil
}
