import type { ScopeAssessment, Snapshot } from '../api.generated';
import type { Page } from '../App';
import { Status } from '../components/ui';

export function scopeRequests(snapshot: Snapshot, taskId?: string): ScopeAssessment[] {
  return (snapshot.scope_requests ?? []).filter((request) => taskId === undefined || request.task_id === taskId);
}

export function ScopeRequests({ snapshot, taskId, requests = scopeRequests(snapshot, taskId), navigate }: {
  snapshot: Snapshot; taskId?: string; requests?: ScopeAssessment[]; navigate?: (page: Page, id?: string) => void;
}) {
  function sourceLink(page: Page, id: string, label: string) {
    return <a className="text-button" href={`#${page}?id=${encodeURIComponent(id)}`} onClick={navigate ? (event) => { event.preventDefault(); navigate(page, id); } : undefined}>{label} →</a>;
  }
  return <section className="detail-section scope-requests" aria-label="范围请求历史"><h3>范围请求历史 <span className="count">{requests.length}</span></h3>
    <p className="muted">请求状态不代表范围已获授权；实际授权以任务契约为准。</p>
    {requests.length ? requests.map((request) => <article key={request.id}>
      <details><summary>技术详情</summary><code>{request.id}</code></details> · 请求 v{request.version} <Status value={request.state} />
      <p className="preserve-lines">{request.reason}</p>
      <div className="code-list">{(request.paths ?? []).map((path, index) => <code key={index}>{path}</code>)}</div>
      <p>任务 {snapshot.tasks?.find((task) => task.id === request.task_id)?.title || '未命名任务'}</p><details><summary>技术详情：授权</summary><p>任务 {request.task_id} · 契约 v{request.task_version} · 授权 v{request.authorization_version} · 来源 Run 代次 {request.generation}</p>
      <p>目标 v{request.goal_version} · 目标授权 v{request.goal_authorization_version} · 自动化 v{request.automation_version} · 政策 v{request.policy_version}</p></details>
      {sourceLink('runs', request.run_id, '原始 Run')}
      {(snapshot.planning_runs ?? []).filter((run) => run.scope_request_id === request.id).map((run) => <div key={run.id}>{sourceLink('planning', run.id, '关联 PlanningRun')} <Status value={run.state} /></div>)}
    </article>) : <p className="muted">暂无范围请求记录。</p>}
  </section>;
}
