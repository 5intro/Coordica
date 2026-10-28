package domain

// Owner-issued trusted provider identities. A display name, comment body or
// model report cannot enroll itself as a gate or replace a required provider.
type ExternalReviewPolicy struct {
	Provider string   `json:"provider"`
	ActorID  int64    `json:"actor_id"`
	AppID    int64    `json:"app_id"`
	Kinds    []string `json:"kinds"`
}

type ExternalReview struct {
	Provider string `json:"provider"`
	Kind     string `json:"kind"`
	ActorID  int64  `json:"actor_id"`
	AppID    int64  `json:"app_id"`
	ReportID int64  `json:"report_id"`
	Commit   string `json:"commit"`
	State    string `json:"state"`
	URL      string `json:"url"`
	Summary  string `json:"summary"`
	Evidence string `json:"evidence,omitempty"`
}

func ValidateExternalReviewPolicy(policies []ExternalReviewPolicy) error {
	if len(policies) > 4 {
		return Fail("invalid_input", "at most four trusted review sources supported")
	}
	seen := map[string]bool{}
	for _, p := range policies {
		if p.Provider != "codex" || p.ActorID <= 0 || p.AppID <= 0 || len(p.Kinds) < 1 || len(p.Kinds) > 2 || seen[p.Provider] {
			return Fail("invalid_input", "explicit supported provider, positive actor/app identities and unique kinds required")
		}
		seen[p.Provider] = true
		kinds := map[string]bool{}
		for _, kind := range p.Kinds {
			if (kind != "code_review" && kind != "security_review") || kinds[kind] {
				return Fail("invalid_input", "unsupported or duplicate review kind")
			}
			kinds[kind] = true
		}
	}
	return nil
}

func ExternalReviewsReady(policies []ExternalReviewPolicy, reports []ExternalReview, commit string) bool {
	expected := map[string]ExternalReviewPolicy{}
	for _, p := range policies {
		for _, kind := range p.Kinds {
			expected[p.Provider+"/"+kind] = p
		}
	}
	if len(reports) != len(expected) {
		return false
	}
	seen := map[string]bool{}
	for _, r := range reports {
		key := r.Provider + "/" + r.Kind
		p, ok := expected[key]
		if !ok || seen[key] || r.ActorID != p.ActorID || r.AppID != p.AppID || r.Commit != commit || r.ReportID <= 0 || r.State != "success" {
			return false
		}
		seen[key] = true
	}
	return true
}
