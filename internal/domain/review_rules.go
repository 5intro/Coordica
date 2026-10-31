package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path"
	"strings"
	"unicode/utf8"
)

type ReviewRuleFile struct {
	Path string `json:"path"`
	Text string `json:"text"`
}

// Rules come from the fixed target main, not the candidate being reviewed.
type ReviewRules struct {
	Version int              `json:"version"`
	Base    string           `json:"base"`
	Digest  string           `json:"digest"`
	Files   []ReviewRuleFile `json:"files"`
	Changed []string         `json:"changed"`
}

func ReviewRulePath(v string) bool {
	switch path.Base(v) {
	case "AGENTS.md", "REVIEW.md", "QUALITY.md", "SECURITY.md":
		return true
	}
	return false
}

func ReviewRulesDigest(r ReviewRules) string {
	r.Digest = ""
	b, _ := json.Marshal(r)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func ValidateReviewRules(r ReviewRules, base string) error {
	baseBytes, err := hex.DecodeString(base)
	if err != nil || len(baseBytes) != 20 || r.Version != ReviewContractVersion || r.Base != base || len(r.Files) > 64 || len(r.Changed) > 128 ||
		r.Digest != ReviewRulesDigest(r) {
		return Fail("invalid_context", "review rules require exact base, version and digest")
	}
	size, previous := 0, ""
	for _, file := range r.Files {
		if !validRulePath(file.Path) || file.Path <= previous || !utf8.ValidString(file.Text) || strings.ContainsRune(file.Text, 0) {
			return Fail("invalid_context", "invalid or duplicate pinned rule file")
		}
		size += len(file.Text)
		previous = file.Path
	}
	previous = ""
	for _, file := range r.Changed {
		if !validRulePath(file) || file <= previous {
			return Fail("invalid_context", "invalid or duplicate changed rule path")
		}
		previous = file
	}
	if size > 64<<10 {
		return Fail("context_limit", "complete review rules exceed 64 KiB")
	}
	return nil
}

func validRulePath(v string) bool {
	return ChatText(v, 1024, true) && ReviewRulePath(v) && path.Clean(v) == v &&
		!strings.HasPrefix(v, "/") && !strings.HasPrefix(v, "../") && !strings.Contains(v, "\\")
}
