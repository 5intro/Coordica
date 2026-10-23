package domain

import (
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Signoff is an Owner declaration about one immutable coverage view. It never
// changes requirement/task state, supplies test success or authorizes execution.
type AcceptanceSignoff struct {
	ID             string    `json:"id"`
	Version        int64     `json:"version"`
	RequirementID  string    `json:"requirement_id"`
	Revision       int64     `json:"revision"`
	PlanVersion    int64     `json:"plan_version"`
	Commit         string    `json:"commit"`
	CoverageDigest string    `json:"coverage_digest"`
	ManualCriteria []string  `json:"manual_criteria"`
	State          string    `json:"state"`
	Note           string    `json:"note"`
	ActorID        string    `json:"actor_id"`
	RecordedAt     time.Time `json:"recorded_at"`
}

type AcceptanceSignoffRequest struct {
	RequirementID   string   `json:"requirement_id"`
	Commit          string   `json:"commit"`
	Action          string   `json:"action"`
	ExpectedVersion int64    `json:"expected_version"`
	ExpectedDigest  string   `json:"expected_digest"`
	ManualCriteria  []string `json:"manual_criteria"`
	Note            string   `json:"note"`
}

type AcceptanceSignoffStatus struct {
	RequirementID string              `json:"requirement_id"`
	Commit        string              `json:"commit"`
	Digest        string              `json:"digest"`
	CanAccept     bool                `json:"can_accept"`
	Accepted      bool                `json:"accepted"`
	Receipt       *AcceptanceSignoff  `json:"receipt"`
	Recent        []AcceptanceSignoff `json:"recent"` // newest ten declarations, including revocations
}

var coverageDigestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func ValidateAcceptanceSignoffRequest(in AcceptanceSignoffRequest) error {
	if !ChatIdentifier.MatchString(in.RequirementID) || in.ExpectedVersion < 0 ||
		!utf8.ValidString(in.Note) || strings.TrimSpace(in.Note) == "" || len(in.Note) > 4096 || len(in.ManualCriteria) > 32 {
		return Fail("invalid_input", "需要有效需求、版本和 4096 字节以内的验收说明")
	}
	if in.Action != "accept" && in.Action != "revoke" {
		return Fail("invalid_input", "验收操作无效")
	}
	if in.Action == "accept" && !coverageDigestPattern.MatchString(in.ExpectedDigest) ||
		in.Action == "revoke" && (in.ExpectedDigest != "" || len(in.ManualCriteria) != 0) {
		return Fail("invalid_input", "签收需要覆盖摘要；撤销只绑定原签收版本")
	}
	seen := map[string]bool{}
	for _, id := range in.ManualCriteria {
		if !ChatIdentifier.MatchString(id) || seen[id] {
			return Fail("invalid_input", "人工验收条款无效或重复")
		}
		seen[id] = true
	}
	return nil
}
