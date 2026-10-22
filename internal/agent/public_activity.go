package agent

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
)

// PublicActivity is a deliberately small projection of Codex JSONL. Never
// forward reasoning, command arguments, tool output, raw JSON or approvals.
type PublicActivity struct{ Key, Kind, Text string }

func CodexPublicActivity(event Event) (PublicActivity, bool) {
	activity := PublicActivity{Key: event.Type}
	switch event.Type {
	case "process.started":
		activity.Kind, activity.Text = "started", "Leader 进程已启动"
	case "turn.started":
		activity.Kind, activity.Text = "started", "开始处理当前请求"
	case "turn.completed":
		activity.Kind, activity.Text = "turn_finished", "工具回合已结束，等待中心核对"
	case "turn.failed":
		activity.Kind, activity.Text = "turn_failed", "工具报告回合失败"
	case "item.started", "item.completed":
		var envelope struct {
			Item struct{ ID, Type, Text string } `json:"item"`
		}
		if json.Unmarshal(event.Data, &envelope) != nil || envelope.Item.ID == "" {
			return activity, false
		}
		item := envelope.Item
		activity.Key += ":" + fmt.Sprintf("%x", sha256.Sum256([]byte(item.ID)))
		switch item.Type {
		case "agent_message":
			if event.Type != "item.completed" {
				return activity, false
			}
			activity.Kind, activity.Text = "message", item.Text
			trimmed := strings.TrimSpace(item.Text)
			if json.Valid([]byte(trimmed)) || strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") ||
				strings.HasPrefix(trimmed, "```") {
				activity.Kind, activity.Text = "result_received", "结构化结果已返回，等待中心核对"
			}
		case "command_execution", "mcp_tool_call", "web_search", "file_change":
			activity.Kind, activity.Text = "tool_started", "正在执行工具操作"
			if event.Type == "item.completed" {
				activity.Kind, activity.Text = "tool_completed", "工具操作已返回"
			}
		case "todo_list", "plan":
			activity.Kind, activity.Text = "plan", "工作计划已更新"
		default:
			return activity, false
		}
	default:
		return activity, false
	}
	return activity, true
}
