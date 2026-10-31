package app

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func TestPublicationAliasedReviewConfigurationRollsBackAtomically(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	p := f.snapshot(t).Project
	p.Paused, p.RepositoryURL = true, "https://github.com/owner/repo.git"
	if err := f.s.DB.Update(ctx, func(tx *store.Tx) error { return tx.Put("project", p.ID, p) }); err != nil {
		t.Fatal(err)
	}
	before := f.snapshot(t)
	in := domain.PublicationRequest{ExpectedVersion: p.Version, Policy: domain.PublicationPolicy{
		Mode: "github", RequiredChecks: []string{"ci"}, ReviewProviders: []domain.ExternalReviewPolicy{{Provider: "codex", ActorID: 42, AppID: 99,
			Kinds: []string{"security_review"}, CheckNames: map[string]string{"security_review": "CI"}}}}}
	_, err := f.s.ConfigurePublication(ctx, f.owner, "bad-gate-alias", in)
	var invalid *domain.Error
	if !errors.As(err, &invalid) || invalid.Code != "invalid_input" {
		t.Fatal("Owner API did not reject the ambiguous gate namespace", err)
	}
	after := f.snapshot(t)
	if after.Project.Version != before.Project.Version || !reflect.DeepEqual(after.Project.Publication, before.Project.Publication) || len(after.Events) != len(before.Events) {
		t.Fatal("invalid policy changed project state or audit", after.Project)
	}
}
