// Package testreport interprets bounded structured process output. It grants
// no task authority and never turns command success into case-level evidence.
package testreport

import (
	"bufio"
	"encoding/json"
	"strings"
)

type goEvent struct {
	Action  string
	Package string
	Test    string
}

// GoCase requires a complete package terminal event and an actual test run.
// Skips, repeated failures and clipped/malformed output cannot become green.
func GoCase(output, externalID string) string {
	parts := strings.Split(externalID, "#")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || len(output) >= 256<<10 {
		return "unreported"
	}
	scanner := bufio.NewScanner(strings.NewReader(output))
	scanner.Buffer(make([]byte, 4096), 256<<10)
	started, passed, bad, finished := 0, 0, "", false
	for scanner.Scan() {
		var event goEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil || event.Action == "" || event.Package == "" {
			return "unreported"
		}
		if event.Package != parts[0] {
			continue
		}
		if event.Test == "" {
			if event.Action == "fail" || event.Action == "skip" {
				return "failed"
			}
			finished = finished || event.Action == "pass"
		} else if event.Test == parts[1] {
			switch event.Action {
			case "run":
				started++
			case "pass":
				passed++
			case "fail":
				bad = "failed"
			case "skip":
				if bad == "" {
					bad = "skipped"
				}
			}
		}
	}
	if scanner.Err() != nil || !finished || started == 0 || started != passed && bad == "" {
		return "unreported"
	}
	if bad != "" {
		return bad
	}
	return "passed"
}
