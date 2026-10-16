import type { Snapshot, Run, Candidate, Node, Capability, Task, Decision, ScopeAssessment } from '../api.generated';
import type { Page } from '../App';
import { scopeRequests } from './ScopeRequests';
export const currentWork = (run: Run) => ['starting', 'running', 'cancel_requested'].includes(run.state) || run.state === 'lost' && !run.quiescent;
export const matches = (query: string, values: unknown[]) => query.toLocaleLowerCase().trim().split(/\s+/).every((word) => values.join(' ').toLocaleLowerCase().includes(word));
export function protectedCandidate(snapshot: Snapshot, candidate: Candidate) {
  const task = (snapshot.tasks ?? []).find((t) => t.id === candidate.task_id);
  // Task version advances during execution; approval is fenced by authorization and fixed C.
  return !!task && task.current_candidate_id === candidate.id && task.state === 'awaiting_integration' && !task.hold_reason
    && task.approved_version > 0 && task.authorization_version > 0 && task.authorization_version === candidate.authorization_version
    && candidate.policy_version === snapshot.project?.policy_version
    && candidate.protected && !!candidate.commit && candidate.approved_commit !== candidate.commit
    && ['constructing', 'reviewing', 'validating', 'ready_to_land'].includes(candidate.state);
}
export function agentRows(snapshot: Snapshot): { node: Node; capability: Capability | undefined; toolId: string }[] {
  return (snapshot.nodes ?? []).flatMap((node) => {
    const tools = [...new Set([...(node.tools ?? []).map((p) => p.tool),...(node.capabilities ?? []).filter((c) => c.installed || c.available).map((c) => c.tool), ...(snapshot.runs ?? []).filter((r) => r.node_id === node.id && currentWork(r)).map((r) => r.tool)])];
    return tools.length ? tools.map((toolId) => ({ node, toolId, capability: (node.capabilities ?? []).find((c) => c.tool === toolId) })) : [{ node, toolId: '', capability: undefined }];
  });
}
export function sourcePage(kind: string): Page {
  return kind === 'task' ? 'tasks' : kind === 'run' ? 'runs' : kind === 'candidate' ? 'candidates' : 'planning';
}
type BlockRecord = { id: string; source: string; reason: string; evidence: string; task: Task | undefined; page: Page; target: string; next: string; decision?: Decision; scopeRequests?: ScopeAssessment[] };
export function blockRecords(snapshot: Snapshot): BlockRecord[] {
  const tasks = snapshot.tasks ?? [];
  const taskRows = tasks.filter((task) => task.hold_reason).map((task) => ({ id: `task:${task.id}`, source: '任务阻断', reason: task.hold_reason, evidence: `契约 v${task.version} · 授权 v${task.authorization_version}`, task, page: 'tasks' as Page, target: task.id, next: '查看任务授权、范围与预算' }));
  const runs = (snapshot.runs ?? []).filter((run) => run.state === 'lost' && !run.quiescent).map((run) => ({ id: `run:${run.id}`, source: '失联 Run', reason: '失联 · 未确认静止', evidence: `${run.id} · ${run.node_id} · ${run.detail || '无补充证据'}`, task: tasks.find((task) => task.id === run.task_id), page: 'runs' as Page, target: run.id, next: '核对进程，在运行页记录停止证据' }));
  const candidates = (snapshot.candidates ?? []).filter((candidate) => protectedCandidate(snapshot, candidate)).map((candidate) => ({ id: `candidate:${candidate.id}`, source: '固定候选', reason: '受保护变化等待固定 C 批准', evidence: `C ${candidate.commit} · ${candidate.state} · ${candidate.detail}`, task: tasks.find((task) => task.id === candidate.task_id), page: 'candidates' as Page, target: candidate.id, next: candidate.state === 'ready_to_land' ? '在候选页核验并特别批准此 C' : '查看候选，等待评审与门禁' }));
  const decisions = (snapshot.decisions ?? []).filter((d) => d.status === 'pending' || d.status === 'open');
  const server = (snapshot.blocks ?? []).filter((b) => (b.kind !== 'candidate' || (snapshot.candidates ?? []).some((candidate) => candidate.id === b.object_id && protectedCandidate(snapshot, candidate))) && !decisions.some((d) => d.source_kind === b.kind && d.source_id === b.object_id || b.kind === 'decision' && b.object_id === d.id)).map((b) => ({ id: `${b.kind}:${b.object_id}`, source: b.kind, reason: b.reason, evidence: '', task: tasks.find((t) => t.id === b.object_id), page: sourcePage(b.kind), target: b.object_id, next: `来源操作：${(b.actions ?? []).join('、') || '查看来源'}` }));
  const legacy = [...taskRows, ...runs, ...candidates].filter((row) => !server.some((b) => b.id === row.id) && !decisions.some((d) => `${d.source_kind}:${d.source_id}` === row.id));
  const decisionRows = decisions.map((d) => ({ id: `decision:${d.id}`, source: d.source_kind, reason: d.kind, evidence: d.evidence, task: tasks.find((t) => t.id === d.source_id), page: sourcePage(d.source_kind), target: d.source_id, next: `来源 v${d.source_version}`, decision: d }));
  const rows: BlockRecord[] = [...legacy, ...server, ...decisionRows];
  // A server request block and its task hold describe the same logical block.
  const requestTask = (kind: string, id: string) => kind === 'scope_request' ? scopeRequests(snapshot).find((request) => request.id === id)?.task_id : undefined;
  return rows.filter((row) => {
    if (row.decision) return true;
    if (row.page === 'tasks' && rows.some((other) => other.decision && requestTask(other.source, other.target) === row.target)) return false;
    const taskId = requestTask(row.source, row.target);
    return !taskId || !rows.some((other) => other !== row && other.target === taskId && other.page === 'tasks');
  }).map((row) => {
    const taskId = row.task?.id ?? requestTask(row.source, row.target);
    const requests = taskId ? scopeRequests(snapshot, taskId) : [];
    return { ...row, scopeRequests: requests, ...(taskId && row.source === 'scope_request' ? { task: tasks.find((task) => task.id === taskId), page: 'tasks' as Page, target: taskId } : {}) };
  });
}
