import type { FormEvent } from 'react';
import type { Node, Requirement, Snapshot, Task } from '../api.generated';
import { ApiClient } from '../api/client';
import { Feedback, Field, lines } from '../components/ui';
import { useAction } from '../hooks/useAction';

export function TaskForm({ requirements, tasks, nodes, snapshot, client, refresh, onCreated, onCancel }: { requirements: Requirement[]; tasks: Task[]; nodes: Node[]; snapshot: Pick<Snapshot, 'planning_document' | 'automation'> & Partial<Pick<Snapshot, 'runs'>>; client: ApiClient; refresh: () => void; onCreated: (id: string) => void; onCancel: () => void }) {
  const action = useAction(refresh);
  const tools = [...new Set(nodes.filter((node) => !node.revoked).flatMap((node) => (node.capabilities ?? [])
    .filter((capability) => capability.available && capability.structured && capability.cancel)
    .map((capability) => capability.tool)))].sort();
  const failedRuns = (snapshot.runs ?? []).filter((run) => run.kind === 'verification' && run.state === 'failed');
  const hasGoal = Boolean(snapshot.planning_document);
  const developmentTool = snapshot.automation.development_tool;
  const toolHint = hasGoal
    ? `当前目标的实现工具由中心设置固定为 ${developmentTool || '未配置'}。${tools.includes(developmentTool) ? '节点能力报告不代表已获授权或可立即执行，执行前置条件仍由中心检查。' : '等待未撤销节点报告该工具可用并支持结构化协议与取消；中心允许时可先保存草稿，执行前置条件仍由中心检查。'}`
    : '从节点报告的可用工具中选择；节点尚未上线时也可填写工具名称保存草稿。';
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); const data = new FormData(event.currentTarget);
    if (hasGoal && !developmentTool) return;
    const body: Pick<Task, 'title' | 'description' | 'acceptance' | 'allowed_paths' | 'hot_resources' | 'requirement_ids' | 'dependencies' | 'tool' | 'budget_seconds' | 'max_run_seconds' | 'max_repairs' | 'max_retries' | 'test_run_id'> = {
      title: String(data.get('title')).trim(), description: String(data.get('description')).trim(), acceptance: String(data.get('acceptance')).trim(),
      allowed_paths: lines(String(data.get('paths'))), hot_resources: lines(String(data.get('resources'))),
      requirement_ids: data.getAll('requirements').map(String), dependencies: data.getAll('dependencies').map(String),
      tool: hasGoal ? developmentTool : String(data.get('tool')), budget_seconds: Math.round(Number(data.get('budget')) * 3600), max_run_seconds: Math.round(Number(data.get('run_budget')) * 60),
      test_run_id: String(data.get('test_run_id') ?? ''), max_repairs: Number(data.get('repairs')), max_retries: Number(data.get('retries')),
    };
    await action.execute(async () => { const task = await client.write<Task>('/tasks', body); onCreated(task.id); }, '任务已保存为草稿；批准与执行前置条件由中心检查。');
  }
  return <form className="panel form-stack" onSubmit={submit}><div className="subheading"><h2>新任务</h2><button type="button" className="text-button" onClick={onCancel}>取消</button></div><Field label="任务标题"><input name="title" required autoFocus /></Field><Field label="关联失败测试（可选）" hint="修复仍是普通代码任务，保存草稿不自动批准。"><select name="test_run_id"><option value="">不关联失败测试</option>{failedRuns.map((run) => <option key={run.id} value={run.id}>{run.test_report?.suite_id ?? run.id} · {run.test_report?.commit.slice(0, 12) ?? run.id} · {run.id}</option>)}</select></Field><Field label="目标与非目标"><textarea name="description" required rows={4} /></Field><Field label="可观察验收条件"><textarea name="acceptance" required rows={3} /></Field><div className="form-grid"><Field label="实现工具" hint={toolHint}>{hasGoal ? <input key="goal-tool" name="tool" required readOnly value={developmentTool} /> : <input key="discovered-tool" name="tool" list="discovered-tools" required defaultValue={tools[0] ?? ''} autoComplete="off" />}<datalist id="discovered-tools">{(hasGoal ? tools.filter((tool) => tool === developmentTool) : tools).map((tool) => <option key={tool} value={tool} />)}</datalist></Field><Field label="允许修改的路径" hint="仓库相对路径，一行一个模式。"><textarea name="paths" required rows={3} placeholder="web/src/**" /></Field><Field label="热点资源" hint="一行一个标识，普通路径无需排他。"><textarea name="resources" rows={2} /></Field><Field label="任务累计预算（小时）"><input name="budget" type="number" max="8760" min="0.1" step="0.1" defaultValue="4" required /></Field><Field label="单 Run 上限（分钟）"><input name="run_budget" type="number" max="10080" min="1" defaultValue="60" required /></Field><Field label="自动修复轮数"><input name="repairs" type="number" min="0" defaultValue="2" required /></Field><Field label="瞬态重试次数"><input name="retries" type="number" min="0" defaultValue="2" required /></Field></div>{requirements.length > 0 && <fieldset className="selection-set"><legend>关联需求</legend>{requirements.map((item) => <label className="checkbox" key={item.id}><input name="requirements" type="checkbox" value={item.id} />{item.title}</label>)}</fieldset>}{tasks.filter((task) => task.state !== 'cancelled').length > 0 && <fieldset className="selection-set"><legend>前置任务</legend>{tasks.filter((task) => task.state !== 'cancelled').map((task) => <label className="checkbox" key={task.id}><input name="dependencies" type="checkbox" value={task.id} />{task.title}</label>)}</fieldset>}<Feedback {...action} /><div className="form-footer"><small>创建不等于批准，执行权限由中心检查。</small><button className="primary" disabled={action.busy || (hasGoal && !developmentTool)}>{action.busy ? '正在保存…' : '保存草稿'}</button></div></form>;
}
