import { useState, type FormEvent } from 'react';
import type { BudgetRequest, Run, StopConfirmation, Task, Snapshot } from '../api.generated';
import { ApiClient } from '../api/client';
import { Feedback, Field, duration } from '../components/ui';
import { useAction } from '../hooks/useAction';

export function BudgetEditor({ task, client, refresh, onClose }: { task: Task; client: ApiClient; refresh: () => void; onClose: () => void }) {
  const action = useAction(refresh);
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    const request: BudgetRequest = {
      expected_version: task.version,
      budget_seconds: Math.round(Number(data.get('hours')) * 3600),
      max_repairs: Number(data.get('repairs')),
      max_retries: Number(data.get('retries')),
      reason: String(data.get('reason')).trim(),
    };
    if (await action.execute(() => client.write<Task>(`/tasks/${encodeURIComponent(task.id)}/budget`, request), '新的有限预算已批准。')) onClose();
  }
  return <form className="scope-form form-stack" onSubmit={submit}>
    <div className="subheading"><h3>调整预算 · 契约 v{task.version}</h3><button type="button" className="text-button" onClick={onClose}>收起</button></div>
    <p className="muted">已使用 {duration(task.used_seconds)}、修复 {task.repairs} 轮、重试 {task.retries} 次会保留。完成后仍需显式恢复被阻塞的任务。</p>
    <div className="form-grid">
      <Field label="任务累计预算（小时）"><input name="hours" type="number" required min={Math.max(task.used_seconds + task.reserved_seconds, 1) / 3600} max="8760" step="any" defaultValue={task.budget_seconds / 3600} /></Field>
      <Field label="修复轮数上限"><input name="repairs" type="number" required min={task.repairs} max="100" step="1" defaultValue={task.max_repairs} /></Field>
      <Field label="瞬态重试次数上限"><input name="retries" type="number" required min={task.retries} max="100" step="1" defaultValue={task.max_retries} /></Field>
    </div>
    <Field label="预算调整理由"><textarea name="reason" required rows={2} /></Field>
    <Feedback {...action} />
    <button className="primary align-end" disabled={action.busy}>批准预算调整</button>
  </form>;
}

export function StopConfirmationForm({ run, client, refresh, onClose, onRecorded }: { run: Run; client: ApiClient; refresh: () => void; onClose: () => void; onRecorded?: () => void }) {
  const action = useAction(refresh);
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    const request: StopConfirmation = { expected_version: run.version, evidence: String(data.get('evidence')).trim() };
    if (await action.execute(() => client.write<Run>(`/runs/${encodeURIComponent(run.id)}/confirm-stopped`, request), '已记录停止证据，恢复结果以中心状态为准。')) { onRecorded?.(); onClose(); }
  }
  return <form className="panel form-stack" onSubmit={submit}>
    <div className="subheading"><h2>确认失联进程已停止</h2><button type="button" className="text-button" onClick={onClose}>收起</button></div>
    <code>{run.id} · v{run.version}</code>
    <p className="muted">请先在执行电脑核对进程及其子进程。此确认只释放未知运行占用，已计入的预算会保留。</p>
    <Field label="实际检查证据" hint="记录在哪台电脑、如何确认进程已经静止。"><textarea name="evidence" required rows={3} /></Field>
    <Feedback {...action} />
    <button className="primary align-end" disabled={action.busy}>记录已停止证据</button>
  </form>;
}

// Owner observations and retries remain separate versioned mutations. A successful
// observation never locally manufactures the center's preparation_stopped fact.
export function PreparationRecovery({ task, snapshot, client, refresh }: { task: Task; snapshot: Snapshot; client: ApiClient; refresh: () => void }) {
  const action = useAction(refresh);
  const candidate = snapshot.candidates?.find((item) => item.id === task.current_candidate_id && item.task_id === task.id);
  if (task.verification || task.state !== 'awaiting_integration' || !candidate || candidate.state !== 'rejected' || candidate.commit) return null;
  const source = snapshot.runs?.find((run) => run.id === candidate.source_run_id);
  const quiet = !(snapshot.runs ?? []).some((run) => run.task_id === task.id && !run.quiescent);
  const original = source?.task_id === task.id && source.kind === 'implement' && source.state === 'succeeded' && source.quiescent
    && !!candidate.base && !!candidate.head && source.base === candidate.base && source.head === candidate.head
    && source.authorization_version === task.authorization_version && source.policy_version === snapshot.project?.policy_version
    && source.goal_version === task.goal_version && source.goal_authorization_version === task.goal_authorization_version;
  const authority = task.approved_version > 0 && task.authorization_version > 0 && candidate.authorization_version === task.authorization_version
    && candidate.policy_version === snapshot.project?.policy_version && candidate.baseline_version === task.baseline_version && candidate.baseline_digest === task.baseline_digest;
  const retryable = quiet && !task.current_run_id && original && authority && candidate.preparation_stopped === true
    && task.retries < task.max_retries && task.used_seconds + task.reserved_seconds < task.budget_seconds
    && ['', 'paused', 'human_decision'].includes(task.hold_reason);
  return <details className="detail-section"><summary>高级恢复 · 候选准备失败</summary>
    <p>仅 Owner 可操作。请在实际执行主机核验准备进程及子进程；此处不会自动判断进程已停止。固定候选的审查或测试拒绝不能通过准备重试处理。</p>
    {candidate.preparation_stopped === true ? <p>中心已记录准备停止事实。</p> : <PreparationStopObservation key={candidate.id} task={task} quiet={quiet} client={client} refresh={refresh} />}

    {!quiet && <p>仍有 Run 未确认静止，请先核验并使用原运行恢复入口。</p>}
    <p>重试要求当前原始交付材料、授权与政策有效、Run 静止、准备停止有记录，且预算及重试未耗尽。中心仍将校验不可见的原材料与全部恢复规则。</p>
    <button className="secondary" disabled={action.busy || !retryable} onClick={() => void action.execute(() => client.write(`/tasks/${encodeURIComponent(task.id)}/retry-integration`, { expected_version: task.version }), '已请求有限准备重试，以中心返回状态为准。')}>重试候选准备</button>
    <Feedback {...action} />
  </details>;
}

function PreparationStopObservation({ task, quiet, client, refresh }: { task: Task; quiet: boolean; client: ApiClient; refresh: () => void }) {
  const action = useAction(refresh);
  const [observedVersion, setObservedVersion] = useState(task.version);
  const [evidence, setEvidence] = useState('');
  async function record(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!quiet || !evidence.trim()) return;
    await action.execute(() => client.write(`/tasks/${encodeURIComponent(task.id)}/confirm-preparation-stopped`, { expected_version: observedVersion, evidence: evidence.trim() }), '已记录准备进程停止观察；请刷新核对中心状态后再重试。');
  }
  return <form className="form-stack" onSubmit={record}>
    <p>观察对应任务契约 v{observedVersion}</p>
    {observedVersion !== task.version && <button type="button" className="text-button" onClick={() => { setObservedVersion(task.version); setEvidence(''); }}>重新核对当前任务版本</button>}
    <Field label="准备进程停止的实际检查证据"><textarea name="evidence" required maxLength={16384} rows={3} value={evidence} onChange={(event) => setEvidence(event.target.value)} /></Field>
    <button className="secondary" disabled={action.busy || !quiet || !evidence.trim()}>记录准备进程已停止观察</button>
    <Feedback {...action} />
  </form>;
}
