package agent

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
)

// LeaderPublicActivity deliberately excludes reasoning, protocol arguments,
// streamed partial JSON, file contents, tool output and permission payloads.
func LeaderPublicActivity(event Event) (PublicActivity, bool) {
	if activity, ok := CodexPublicActivity(event); ok {
		return activity, true
	}
	activity := PublicActivity{Key: fmt.Sprintf("%s:%x", event.Type, sha256.Sum256(event.Data))}
	switch event.Type {
	case "acp.message":
		var message struct {
			Method string
			Params struct {
				Update struct {
					Kind   string `json:"sessionUpdate"`
					Status string
				}
			}
		}
		if json.Unmarshal(event.Data, &message) != nil || message.Method != "session/update" {
			return activity, false
		}
		switch message.Params.Update.Kind {
		case "tool_call", "tool_call_update":
			activity.Kind, activity.Text = "tool_started", "正在执行工具操作"
			if message.Params.Update.Status == "completed" || message.Params.Update.Status == "failed" {
				activity.Kind, activity.Text = "tool_completed", "工具操作已返回"
			}
		case "plan":
			activity.Kind, activity.Text = "plan", "工作计划已更新"
		default:
			return activity, false
		}
	case "tool_execution_start":
		activity.Kind, activity.Text = "tool_started", "正在执行工具操作"
	case "tool_execution_end":
		activity.Kind, activity.Text = "tool_completed", "工具操作已返回"
	case "assistant", "message_end":
		// Only a generic completion fact: these messages can contain structured
		// proposals or mixed thinking/tool blocks, unlike Codex's agent_message.
		activity.Kind, activity.Text = "result_received", "Leader 已返回消息，等待中心核对"
	default:
		return activity, false
	}
	return activity, true
}
