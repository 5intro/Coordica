package domain

import "testing"

func TestPublicationPolicyValidation(t *testing.T) {
	base := Project{RepositoryURL: "https://github.com/example/repo.git", Publication: &PublicationPolicy{Mode: "github", RequiredChecks: []string{"ci"}}}
	if e := ValidatePublication(base); e != nil {
		t.Fatal(e)
	}
	for _, mutate := range []func(*Project){
		func(p *Project) { p.RepositoryURL = "https://github.com/owner/repo/extra" },
		func(p *Project) { p.RepositoryURL = "https://owner:secret@github.com/owner/repo.git" },
		func(p *Project) { p.RepositoryURL = "https://github.com.attacker.invalid/owner/repo.git" },
		func(p *Project) { p.RepositoryURL = "https://github.com/owner/repo.git?token=secret" },
		func(p *Project) { p.Publication = &PublicationPolicy{Mode: "github"} },
		func(p *Project) {
			p.Publication = &PublicationPolicy{Mode: "github", RequiredChecks: []string{"ci", "ci"}}
		},
		func(p *Project) {
			p.Publication = &PublicationPolicy{Mode: "github", RequiredChecks: []string{ReviewCheckContext}}
		},
		func(p *Project) { p.Publication = &PublicationPolicy{Mode: "git", RequiredChecks: []string{"ci"}} },
	} {
		p := base
		mutate(&p)
		if e := ValidatePublication(p); e == nil {
			t.Fatal("invalid policy accepted", p.RepositoryURL, p.Publication)
		}
	}
	if e := ValidatePublication(Project{RepositoryURL: "/local/repo"}); e != nil {
		t.Fatal("legacy local mode rejected", e)
	}
}
