package acpclient

import "strings"

func statusFixtureUpdates() []map[string]any {
	return []map[string]any{
		{"sessionUpdate": "plan", "entries": []any{map[string]any{
			"content": "private-plan-body", "priority": "high", "status": "completed", "_meta": nil,
		}}},
		{"sessionUpdate": "available_commands_update", "availableCommands": []any{map[string]any{
			"name": "private-execute-command", "description": "private-command-description",
			"input": map[string]any{"hint": "private-command-hint", "_meta": map[string]any{"private": true}},
		}}},
		{"sessionUpdate": "current_mode_update", "currentModeId": "private-bypass-permissions"},
	}
}

func emitStatusFixture() {
	for _, update := range statusFixtureUpdates() {
		printUpdateFixture("native-session", update)
	}
}

func statusPromptFixture(scenario string, id int) {
	updates := statusFixtureUpdates()
	if strings.HasPrefix(scenario, "status-wrong-session-") {
		for _, update := range updates {
			if update["sessionUpdate"] == strings.TrimPrefix(scenario, "status-wrong-session-") {
				printUpdateFixture("private-other-session", update)
				return
			}
		}
	}
	if scenario == "status-flood" {
		for i := 0; i < 33; i++ {
			printUpdateFixture("native-session", updates[i%len(updates)])
		}
		return
	}
	if scenario == "status-oversize" {
		printUpdateFixture("native-session", updates[0])
		updates[2]["currentModeId"] = strings.Repeat("x", maxFrameBytes)
		printUpdateFixture("native-session", updates[2])
		return
	}
	switch scenario {
	case "status-bad-plan":
		updates[0]["entries"] = []any{map[string]any{"content": "private-invalid-plan", "priority": "high", "status": "unknown"}}
		printUpdateFixture("native-session", updates[0])
		return
	case "status-bad-command":
		updates[1]["availableCommands"] = []any{map[string]any{"name": "private-invalid-command", "description": "", "input": map[string]any{}}}
		printUpdateFixture("native-session", updates[1])
		return
	case "status-bad-mode":
		updates[2]["currentModeId"] = nil
		printUpdateFixture("native-session", updates[2])
		return
	case "status-large":
		updates[1]["availableCommands"].([]any)[0].(map[string]any)["description"] = strings.Repeat("private-large-", (maxFrameBytes-2048)/14)
	}
	for _, update := range updates {
		printUpdateFixture("native-session", update)
	}
	printUpdateFixture("native-session", map[string]any{
		"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "private-known-text"},
	})
	if scenario == "status-normal" {
		waitFixtureContinue()
	}
	cancelFixtureTerminal(id, "end_turn")
}
