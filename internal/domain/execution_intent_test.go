package domain

import (
	"strings"
	"testing"
)

func TestExecutionIntentStrictBoundary(t *testing.T) {
	valid := `{"version":1,"status":"scope_requested","requested_paths":["src/greeting.mjs"],"reason":"Existing task needs source","summary":"Stopped before editing"}`
	for _, text := range []string{valid, `{"version":1,"status":"completed","requested_paths":[],"reason":"","summary":"Implemented and tested"}`, `{"version":1,"status":"blocked","requested_paths":[],"reason":"Major architecture change","summary":"Owner decision required"}`} {
		if _, err := ParseExecutionIntent(text); err != nil {
			t.Fatal(err)
		}
	}
	for _, text := range []string{
		"Need src/greeting.mjs; stopped", "```json\n" + valid + "\n```", valid + " {}", valid + " prose", valid + "\x00",
		strings.Replace(valid, `"version":1`, `"version":2`, 1),
		strings.Replace(valid, `"version":1`, `"version":"1"`, 1),
		strings.Replace(valid, `"version":1`, `"version":null`, 1),
		strings.Replace(valid, `"status":"scope_requested"`, `"status":"approved"`, 1),
		strings.Replace(valid, `"status":"scope_requested"`, `"status":null`, 1),
		strings.Replace(valid, `"version":1,`, `"version":1,"version":1,`, 1),
		strings.Replace(valid, `"version":1,`, `"grant":true,"version":1,`, 1),
		strings.Replace(valid, `"version":1,`, "", 1),
		strings.Replace(valid, `["src/greeting.mjs"]`, `null`, 1),
		strings.Replace(valid, `["src/greeting.mjs"]`, `[null]`, 1),
		strings.Replace(valid, `["src/greeting.mjs"]`, `[4]`, 1),
		strings.Replace(valid, `["src/greeting.mjs"]`, `["src/greeting.mjs","src/greeting.mjs"]`, 1),
		strings.Replace(valid, `["src/greeting.mjs"]`, `{}`, 1),
		strings.Replace(valid, `["src/greeting.mjs"]`, `[]`, 1),
		strings.Replace(valid, `"reason":"Existing task needs source"`, `"reason":null`, 1),
		strings.Replace(valid, `"reason":"Existing task needs source"`, `"reason":false`, 1),
		strings.Replace(valid, `"reason":"Existing task needs source"`, `"reason":" "`, 1),
		strings.Replace(valid, `"summary":"Stopped before editing"`, `"summary":null`, 1),
		strings.Replace(valid, `"summary":"Stopped before editing"`, `"summary":""`, 1),
		strings.Replace(valid, `"status":"scope_requested"`, `"status":"completed"`, 1),
		strings.Replace(valid, `"status":"scope_requested"`, `"status":"blocked"`, 1),
		strings.Replace(valid, "Existing task needs source", strings.Repeat("x", 4097), 1),
		strings.Repeat(" ", 64<<10) + valid,
	} {
		if _, err := ParseExecutionIntent(text); err == nil {
			t.Fatalf("accepted malformed intent: %.200s", text)
		}
	}
}
