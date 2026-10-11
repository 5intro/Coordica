package runner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"coordica/internal/agent"
)

func TestPrivateEvidenceLimitRetainsFinalResultAndMarksOmittedEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "事件 with spaces.jsonl")
	e, err := newEvidence(path, "fixture-node-secret")
	if err != nil {
		t.Fatal(err)
	}
	chunk, _ := json.Marshal(map[string]string{"text": strings.Repeat("structured text fixture-node-secret ", 6000)})
	for i := 0; i < 30; i++ {
		e.append(agent.Event{Type: "agent_message", Data: chunk}, false)
	}
	e.value("runner.result", map[string]string{"output_text": "final fixture-node-secret"}, true)
	if err := e.close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > maxEvidenceBytes || strings.Contains(string(raw), "fixture-node-secret") {
		t.Fatal("log limit or redaction failed")
	}
	if !strings.Contains(string(raw), "runner.log_truncated") || !strings.Contains(string(raw), "runner.result") || !strings.Contains(string(raw), "final [redacted]") {
		t.Fatal("truncation marker or final output missing")
	}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if !json.Valid([]byte(line)) {
			t.Fatal("partial protocol event stored")
		}
	}
	if stat, err := os.Stat(path); err != nil || (runtime.GOOS != "windows" && stat.Mode().Perm()&0077 != 0) {
		t.Fatal("private evidence permissions", err)
	}
}
