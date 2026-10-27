package domain

import "testing"

func TestFailureRecoveryHintCannotApproveOrUseUnknownCategories(t *testing.T) {
	valid := &RecoveryHint{Category: "test_error", Reason: "Fixture appears inconsistent with the fixed contract"}
	if err := ValidateRecoveryHint(valid, "changes_requested"); err != nil {
		t.Fatal(err)
	}
	for _, verdict := range []string{"approve", "arbitrary"} {
		if err := ValidateRecoveryHint(valid, verdict); err == nil {
			t.Fatal("invalid verdict accepted", verdict)
		}
	}
	if err := ValidateRecoveryHint(&RecoveryHint{Category: "permission_granted", Reason: "bypass"}, "blocked"); err == nil {
		t.Fatal("unknown recovery grant accepted")
	}
}
