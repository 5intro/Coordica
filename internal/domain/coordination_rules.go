package domain

import (
	"encoding/hex"
	"slices"
	"strings"
)

func ValidateWorkAnchor(a WorkAnchor) error {
	if (a.Kind != "project" && a.Kind != "requirement" && a.Kind != "task") || !ChatIdentifier.MatchString(a.ID) {
		return Fail("invalid_input", "an explicit project, requirement or task anchor is required")
	}
	return nil
}

func ValidateWorkReference(r WorkReference, pinned bool) error {
	kinds := []string{"project", "requirement", "task", "run", "candidate", "decision", "consultation", "initialization", "goal"}
	if !slices.Contains(kinds, r.Kind) || !ChatIdentifier.MatchString(r.ID) ||
		r.Version < 0 || r.AuthorizationVersion < 0 || r.Generation < 0 || r.Revision < 0 {
		return Fail("invalid_input", "invalid work reference")
	}
	if pinned || r.Stamp != "" {
		b, err := hex.DecodeString(r.Stamp)
		if err != nil || len(b) != 32 || strings.ToLower(r.Stamp) != r.Stamp {
			return Fail("invalid_input", "a current reference stamp is required")
		}
	}
	return nil
}

func validateWorkReferences(refs []WorkReference, pinned bool) error {
	if len(refs) > MaxWorkReferences {
		return Fail("invalid_input", "at most 16 references per request")
	}
	seen := map[string]bool{}
	for _, r := range refs {
		if err := ValidateWorkReference(r, pinned); err != nil {
			return err
		}
		key := r.Kind + ":" + r.ID
		if seen[key] {
			return Fail("invalid_input", "duplicate work reference")
		}
		seen[key] = true
	}
	return nil
}

func ValidateWorkNote(in WorkNoteInput) error {
	if err := ValidateWorkAnchor(in.Anchor); err != nil {
		return err
	}
	if err := ValidateWorkReference(WorkReference{Kind: in.Anchor.Kind, ID: in.Anchor.ID, Stamp: in.ExpectedStamp}, true); err != nil {
		return err
	}
	if in.Kind != "note" && in.Kind != "reference" ||
		!ChatText(in.OriginalText, 16<<10, in.Kind == "note") || !ChatText(in.AssistantSummary, 4<<10, false) ||
		in.Kind == "reference" && (in.OriginalText != "" || in.AssistantSummary != "" || len(in.References) == 0) ||
		in.Corrects != "" && !ChatIdentifier.MatchString(in.Corrects) {
		return Fail("invalid_input", "explicit bounded note or reference required")
	}
	return validateWorkReferences(in.References, true)
}

func ValidateWorkContextQuery(q WorkContextQuery) error {
	if err := ValidateWorkAnchor(q.Anchor); err != nil {
		return err
	}
	if q.Limit < 1 || q.Limit > 100 || len(q.Cursor) > 2048 || len(q.ReferenceCursor) > 2048 {
		return Fail("invalid_input", "bounded work context page required")
	}
	return validateWorkReferences(q.References, false)
}

func ValidateWorkListQuery(q WorkListQuery) error {
	if q.Limit < 1 || q.Limit > 100 || len(q.Cursor) > 2048 || q.Kind != "" && q.Kind != "project" && q.Kind != "requirement" && q.Kind != "task" {
		return Fail("invalid_input", "bounded work list required")
	}
	return nil
}
