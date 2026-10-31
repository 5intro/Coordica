package domain

import (
	"strings"
	"time"
)

// Owner-issued trusted provider identities. A display name, comment body or
// model report cannot enroll itself as a gate or replace a required provider.
type ExternalReviewPolicy struct {
	CheckNames map[string]string `json:"check_names,omitempty"`
	Provider   string            `json:"provider"`
	ActorID    int64             `json:"actor_id"`
	AppID      int64             `json:"app_id"`
	Kinds      []string          `json:"kinds"`
}

type ExternalReview struct {
	CheckState      string    `json:"check_state,omitempty"`
	ReportState     string    `json:"report_state,omitempty"`
	ReportUpdatedAt time.Time `json:"report_updated_at,omitempty"`
	CheckID         int64     `json:"check_id,omitempty"`
	CheckName       string    `json:"check_name,omitempty"`
	Provider        string    `json:"provider"`
	Kind            string    `json:"kind"`
	ActorID         int64     `json:"actor_id"`
	AppID           int64     `json:"app_id"`
	ReportID        int64     `json:"report_id"`
	Commit          string    `json:"commit"`
	State           string    `json:"state"`
	URL             string    `json:"url"`
	Summary         string    `json:"summary"`
	Evidence        string    `json:"evidence,omitempty"`
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
		names := map[string]bool{}
		if len(p.CheckNames) != len(p.Kinds) {
			return Fail("invalid_input", "each review kind requires an explicit App-owned Check Run name")
		}
		for _, kind := range p.Kinds {
			if (kind != "code_review" && kind != "security_review") || kinds[kind] {
				return Fail("invalid_input", "unsupported or duplicate review kind")
			}
			kinds[kind] = true
			name := p.CheckNames[kind]
			if strings.TrimSpace(name) != name || name == "" || len(name) > 256 || strings.ContainsAny(name, "\x00\r\n") || strings.EqualFold(name, ReviewCheckContext) || names[strings.ToLower(name)] {
				return Fail("invalid_input", "distinct nonempty external review check names are required")
			}
			names[strings.ToLower(name)] = true
		}
	}
	return nil
}

// Required tests, internal review and external review are independent controls.
// Reject ambiguous namespaces before configuration or protocol facts can bind a
// review App's check to a required test name.
func ValidateExternalReviewGates(required []string, policies []ExternalReviewPolicy) error {
	if err := ValidateExternalReviewPolicy(policies); err != nil {
		return err
	}
	for _, policy := range policies {
		for _, name := range policy.CheckNames {
			for _, ci := range required {
				if strings.EqualFold(name, strings.TrimSpace(ci)) {
					return Fail("invalid_input", "external review and required CI checks must have distinct names")
				}
			}
		}
	}
	return nil
}

func ExternalReviewsReady(policies []ExternalReviewPolicy, reports []ExternalReview, commit string) bool {
	if ValidateExternalReviewPolicy(policies) != nil {
		return false
	}
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
		if !ok || seen[key] || !ExternalReviewVerdict(p, r, commit, "success") {
			return false
		}
		seen[key] = true
	}
	return true
}

func ExternalReviewVerdict(policy ExternalReviewPolicy, report ExternalReview, commit, verdict string) bool {
	if verdict != "success" && verdict != "failure" || ValidateExternalReviewPolicy([]ExternalReviewPolicy{policy}) != nil {
		return false
	}
	enrolled := false
	for _, kind := range policy.Kinds {
		enrolled = enrolled || kind == report.Kind
	}
	return enrolled && report.Provider == policy.Provider && report.ActorID == policy.ActorID && report.AppID == policy.AppID && report.Commit == commit &&
		report.ReportID > 0 && !report.ReportUpdatedAt.IsZero() && report.CheckID > 0 && report.CheckName == policy.CheckNames[report.Kind] &&
		report.State == verdict && report.CheckState == verdict && report.ReportState == verdict
}
