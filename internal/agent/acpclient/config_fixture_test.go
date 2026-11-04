package acpclient

import (
	"encoding/json"
	"fmt"
	"os"
)

func fixtureConfigOptions(value string) []any {
	return []any{map[string]any{"id": "native-model", "name": "Native model", "type": "select", "category": "vendor_hint", "description": nil, "currentValue": value,
		"options": []any{map[string]any{"group": "family", "name": "Family", "options": []any{
			map[string]any{"value": "small", "name": "Small", "description": "private-choice-description"},
			map[string]any{"value": "large", "name": "Large"},
		}}}}}
}

func configFixtureReply(scenario string, id int, value string) {
	if scenario == "config-pause" {
		waitFixtureContinue()
	}
	if scenario == "config-no-response" {
		return
	}
	if scenario == "config-disconnect" {
		os.Exit(0)
	}
	result := map[string]any{"configOptions": fixtureConfigOptions(value)}
	response := map[string]any{"jsonrpc": "2.0", "id": id, "result": result}
	switch scenario {
	case "config-before":
		printConfigUpdateFixture(fixtureConfigOptions("small"))
	case "config-response-null":
		result["configOptions"] = nil
	case "config-response-missing":
		delete(result, "configOptions")
	case "config-response-wrong":
		result["configOptions"] = fixtureConfigOptions("small")
	case "config-response-empty":
		result["configOptions"] = []any{}
	case "config-response-duplicate":
		o := fixtureConfigOptions(value)
		result["configOptions"] = append(o, o[0])
	case "config-wrong-id":
		response["id"] = id + 1
	case "config-rejected":
		if value == "large" {
			delete(response, "result")
			response["error"] = map[string]any{"code": -32000, "message": "private-config-rejection", "data": map[string]string{"private": "private-config-rejection"}}
		}
	}
	raw, _ := json.Marshal(response)
	fmt.Println(string(raw))
	if scenario == "config-after" {
		printConfigUpdateFixture(fixtureConfigOptions("small"))
	}
	if scenario == "config-response-eof" {
		os.Exit(0)
	}
	if scenario == "config-duplicate-id" {
		fmt.Println(string(raw))
	}
}

func printConfigUpdateFixture(options any) {
	printUpdateFixture("native-session", map[string]any{"sessionUpdate": "config_option_update", "configOptions": options})
}
func configNotificationFixture(scenario string) {
	switch scenario {
	case "config-notify-null":
		printConfigUpdateFixture(nil)
	case "config-notify-missing":
		printUpdateFixture("native-session", map[string]any{"sessionUpdate": "config_option_update"})
	case "config-notify-overflow":
		for i := 0; i < 33; i++ {
			printConfigUpdateFixture(fixtureConfigOptions("small"))
		}
	default:
		printConfigUpdateFixture([]any{})
	}
}
