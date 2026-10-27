import type { ReactNode } from 'react';

const stateLabels: Record<string, string> = {
  draft: '待批准', ready: '可执行', executing: '执行中', awaiting_integration: '待集成',
  verified: '验证完成', integrated: '已集成', cancelled: '已取消', starting: '启动中', running: '运行中',
  cancel_requested: '正在停止', stopped: '已停止', succeeded: '执行完成', failed: '失败',
  lost: '失联', accepted: '已受理', constructing: '构造中', reviewing: '评审中',
  prechecking: '预检查 · 尚未进入审查', validating: '验证中', ready_to_land: '审查通过 · 待集成', landing: '写入中', dispatching: '已派发',
  landed: '已进入主线', rejected: '已拒绝', invalidated: '已失效', reconciling: '对账中',
  paused: '已暂停', approve: '评审通过', changes_requested: '需要修改', blocked: '阻断',
  history_only: '历史活动 · 未保留结束状态', history_unavailable: '历史活动已过期',
};
const green = new Set(['verified', 'integrated', 'succeeded', 'landed', 'approve']);
const active = new Set(['executing', 'starting', 'running', 'reviewing', 'validating', 'landing']);
const red = new Set(['failed', 'lost', 'rejected', 'changes_requested']);
const warning = new Set(['draft', 'paused', 'blocked', 'cancel_requested']);
export const stateLabel = (value: string) => stateLabels[value] ?? value;
export const platformLabel = (value: string) => {
  const [os, ...architecture] = value.split('/');
  const label = ({ darwin: 'macOS', linux: 'Linux', windows: 'Windows' }[os] ?? os);
  return [label, ...architecture].join('/');
};
export const reasonLabel = (value: string) => ({
  requirement_changed: '需求变更待重排', scope: '范围调整', scope_requested: '请求调整范围', scope_wait_stop: '范围调整等待停止核验',
  failure_assessment: '等待 Leader 诊断', failure_followup: '等待关联修复', human_decision: '等待人工决策', offline: '执行主机离线', baseline_changed: '基线已变化', budget: '预算限制', paused: '已暂停', owner: 'Owner 阻断',
  task: '任务', run: '运行', candidate: '候选', decision: '决策', scope_request: '范围请求',
  planning_document: '规划文档', planning_run: '规划运行', unknown_live: '运行状态未知，等待停止核验', critical_test_failure: '关键测试失败', protected_candidate: '受保护候选等待批准', planning_not_ready: '规划尚未就绪', planning_expired: '规划运行已过期', planning_failure: '规划执行失败', invalid_result: '规划结果无效', invalid_plan: '规划方案无效', goal: '目标调整', protected: '受保护变化',
}[value] ?? value);
export const categoryLabel = (value: string) => ({ smoke: '冒烟', regression: '回归', 'white-box': '白盒', 'black-box': '黑盒', e2e: '端到端', 'golden-journey': '黄金旅程', 'long-running': '长生命周期' }[value] ?? value);
export const stageLabel = (value: string) => ({ merge: '合并检查', project: '项目验证', release: '发布回归' }[value] ?? value);
export const roleLabel = (value: string) => ({ implement: '开发', review: '独立审查', repair: '修复', verification: '测试验证' }[value] ?? value);
export function Status({ value }: { value: string }) {
  const tone = green.has(value) ? 'green' : active.has(value) ? 'active' : red.has(value) ? 'red' : warning.has(value) ? 'warning' : 'neutral';
  return <span className={`status ${tone}`}>{stateLabels[value] ?? value}</span>;
}
export function Empty({ title, children }: { title: string; children?: ReactNode }) {
  return <div className="empty"><h3>{title}</h3>{children && <p>{children}</p>}</div>;
}
export function Feedback({ error, notice }: { error?: string; notice?: string }) {
  return <>{error && <p className="feedback error" role="alert">{error}</p>}{notice && <p className="feedback notice" role="status">{notice}</p>}</>;
}
export function SectionTitle({ title, description, actions, filters }: { title: string; description?: string; actions?: ReactNode; filters?: ReactNode }) {
  return <header className={`section-title ${filters ? 'has-filters' : ''}`}><div>{filters ? <div className="section-title-heading"><h1>{title}</h1>{filters}</div> : <h1>{title}</h1>}{description && <p>{description}</p>}</div>{actions && <div className="actions">{actions}</div>}</header>;
}
export function Field({ label, hint, children }: { label: string; hint?: string; children: ReactNode }) {
  return <label className="field"><span>{label}</span>{children}{hint && <small>{hint}</small>}</label>;
}
export function hours(seconds: number) { return (seconds / 3600).toLocaleString('zh-CN', { maximumFractionDigits: 1 }); }
export function duration(seconds: number) {
  if (seconds < 60) return `${seconds} 秒`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)} 分`;
  return `${hours(seconds)} 小时`;
}
export function time(value: string | Date | undefined) {
  if (!value || String(value).startsWith('0001-')) return '尚无记录';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? '时间无效' : new Intl.DateTimeFormat('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false }).format(date);
}
export function short(value: string, length = 10) { return value ? value.slice(0, length) : '尚未生成'; }
export function lines(value: string) { return value.split(/\r?\n/).map((line) => line.trim()).filter(Boolean); }
export function Budget({ used, reserved, total }: { used: number; reserved: number; total: number }) {
  const consumed = total > 0 ? Math.min(100, (used / total) * 100) : 0;
  const held = total > 0 ? Math.min(100 - consumed, (reserved / total) * 100) : 0;
  return <div className="budget"><div className="budget-track" role="meter" aria-label="累计预算" aria-valuemin={0} aria-valuemax={total} aria-valuenow={used}><span style={{ width: `${consumed}%` }} /><span className="reserved" style={{ width: `${held}%` }} /></div><div className="budget-caption"><span>已用 {duration(used)} · 预留 {duration(reserved)}</span><span>上限 {duration(total)}</span></div></div>;
}
