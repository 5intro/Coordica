import type { FormEvent } from 'react';
import type { BudgetRequest, Project, Run, StopConfirmation, Task } from '../api.generated';
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

export function ProjectBudgetEditor({ project, client, refresh, onClose }: { project: Project; client: ApiClient; refresh: () => void; onClose: () => void }) {
  const action = useAction(refresh);
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    const request: BudgetRequest = {
      expected_version: project.version,
      budget_seconds: Math.round(Number(data.get('hours')) * 3600),
      max_repairs: 0,
      max_retries: 0,
      reason: String(data.get('reason')).trim(),
    };
    if (await action.execute(() => client.write<Project>('/project/budget', request), '项目预算已更新，已使用额度保留。')) onClose();
  }
  return <form className="scope-form form-stack" onSubmit={submit}>
    <div className="subheading"><h3>项目预算 · v{project.version}</h3><button type="button" className="text-button" onClick={onClose}>收起</button></div>
    <Field label="项目累计预算（小时）"><input name="hours" type="number" required min={Math.max(project.used_seconds + project.reserved_seconds, 1) / 3600} max="8760" step="any" defaultValue={project.total_budget_seconds / 3600} /></Field>
    <Field label="项目预算调整理由"><textarea name="reason" required rows={2} /></Field>
    <p className="muted">提交会保留已用及预留额度。项目保持暂停，由你核验后恢复。</p>
    <Feedback {...action} />
    <button className="primary align-end" disabled={action.busy}>批准项目预算</button>
  </form>;
}
