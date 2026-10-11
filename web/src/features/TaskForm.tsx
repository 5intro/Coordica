import type { FormEvent } from 'react';
import type { Requirement, Task } from '../api.generated';
import { ApiClient } from '../api/client';
import { Feedback, Field, lines } from '../components/ui';
import { useAction } from '../hooks/useAction';

export function TaskForm({ requirements, tasks, client, refresh, onCreated, onCancel }: { requirements: Requirement[]; tasks: Task[]; client: ApiClient; refresh: () => void; onCreated: (id: string) => void; onCancel: () => void }) {
  const action = useAction(refresh);
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); const data = new FormData(event.currentTarget);
    const body: Pick<Task, 'title' | 'description' | 'acceptance' | 'allowed_paths' | 'hot_resources' | 'requirement_ids' | 'dependencies' | 'tool' | 'budget_seconds' | 'max_run_seconds' | 'max_repairs' | 'max_retries'> = {
      title: String(data.get('title')).trim(), description: String(data.get('description')).trim(), acceptance: String(data.get('acceptance')).trim(),
      allowed_paths: lines(String(data.get('paths'))), hot_resources: lines(String(data.get('resources'))),
      requirement_ids: data.getAll('requirements').map(String), dependencies: data.getAll('dependencies').map(String),
      tool: String(data.get('tool')), budget_seconds: Math.round(Number(data.get('budget')) * 3600), max_run_seconds: Math.round(Number(data.get('run_budget')) * 60),
      max_repairs: Number(data.get('repairs')), max_retries: Number(data.get('retries')),
    };
    await action.execute(async () => { const task = await client.write<Task>('/tasks', body); onCreated(task.id); }, '任务已保存为草稿，批准后可执行。');
  }
  return <form className="panel form-stack" onSubmit={submit}><div className="subheading"><h2>新任务</h2><button type="button" className="text-button" onClick={onCancel}>取消</button></div><Field label="任务标题"><input name="title" required autoFocus /></Field><Field label="目标与非目标"><textarea name="description" required rows={4} /></Field><Field label="可观察验收条件"><textarea name="acceptance" required rows={3} /></Field><div className="form-grid"><Field label="实现工具"><select name="tool" defaultValue="codex"><option value="codex">Codex</option><option value="claude">Claude Code</option><option value="dsh">dsh</option></select></Field><Field label="允许修改的路径" hint="仓库相对路径，一行一个模式。"><textarea name="paths" required rows={3} placeholder="web/src/**" /></Field><Field label="热点资源" hint="一行一个标识，普通路径无需排他。"><textarea name="resources" rows={2} /></Field><Field label="任务累计预算（小时）"><input name="budget" type="number" min="0.1" step="0.1" defaultValue="4" required /></Field><Field label="单 Run 上限（分钟）"><input name="run_budget" type="number" min="1" defaultValue="60" required /></Field><Field label="自动修复轮数"><input name="repairs" type="number" min="0" defaultValue="2" required /></Field><Field label="瞬态重试次数"><input name="retries" type="number" min="0" defaultValue="2" required /></Field></div>{requirements.length > 0 && <fieldset className="selection-set"><legend>关联需求</legend>{requirements.map((item) => <label className="checkbox" key={item.id}><input name="requirements" type="checkbox" value={item.id} />{item.title}</label>)}</fieldset>}{tasks.filter((task) => task.state !== 'cancelled').length > 0 && <fieldset className="selection-set"><legend>前置任务</legend>{tasks.filter((task) => task.state !== 'cancelled').map((task) => <label className="checkbox" key={task.id}><input name="dependencies" type="checkbox" value={task.id} />{task.title}</label>)}</fieldset>}<Feedback {...action} /><div className="form-footer"><small>创建不等于批准，执行权限由中心检查。</small><button className="primary" disabled={action.busy}>{action.busy ? '正在保存…' : '保存草稿'}</button></div></form>;
}
