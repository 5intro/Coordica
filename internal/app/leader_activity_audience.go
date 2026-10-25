package app

import (
	"strconv"

	"coordica/internal/domain"
)

// Redaction cannot prove arbitrary model prose is safe. Limited chat receives
// only closed status labels; original display text remains Owner-only.
func projectLeaderActivityAudience(actor Actor, page *domain.LeaderActivityPage) {
	if actor.Role == "owner" {
		return
	}
	for i := range page.Events {
		event := &page.Events[i]
		event.Kind, event.Text = safeLeaderStatus(event.Kind)
		event.Key = "activity:" + strconv.FormatInt(event.Sequence, 10)
	}
}

func safeLeaderStatus(kind string) (string, string) {
	switch kind {
	case "started":
		return kind, "Leader 开始处理请求"
	case "tool_started":
		return kind, "正在执行工具操作"
	case "tool_completed":
		return kind, "工具操作已返回"
	case "plan":
		return kind, "工作计划已更新"
	case "result_received":
		return kind, "结果已返回，等待中心核对"
	case "turn_finished":
		return kind, "工具回合已结束，等待中心核对"
	case "turn_failed":
		return kind, "工具报告回合失败"
	case "gap":
		return kind, "部分活动已省略"
	default:
		return "message", "Leader 已更新进展"
	}
}
