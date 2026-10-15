import { useState, type ReactNode } from 'react';
import type { Snapshot } from '../api.generated';
import type { Page } from '../App';
import { Status, duration, time } from '../components/ui';
import { blockRecords, currentWork, protectedCandidate } from './management';
import './overview.css';
import { completedTask, terminalTask } from './taskState';
import { platformLabel } from '../components/ui';

type Entry = { id: string; title: string; detail: string; status: ReactNode; page: Page; target: string };
const candidateLabels: Record<string, string> = {
  reviewing: '待审 / 审中', ready_to_land: '审查通过 · 待集成',
};
const roles: Record<string, string> = { implement: '开发', review: '独立审查', repair: '修复', verification: '测试验证' };

export function Overview({ snapshot, navigate }: { snapshot: Snapshot; navigate: (page: Page, id?: string) => void }) {
  const [expandedGoal, setExpandedGoal] = useState(false);
  const project = snapshot.project;
  if (!project) return <div className="overview"><h1>尚无项目</h1><p>等待中心提供项目快照。</p></div>;
  const tasks = snapshot.tasks ?? [];
  const runs = snapshot.runs ?? [];
  const live = runs.filter(currentWork);
  const candidates = (snapshot.candidates ?? []).filter((candidate) => !!candidate.commit && ['reviewing', 'ready_to_land'].includes(candidate.state));
  const blocks = blockRecords(snapshot);
  const taskTitle = (id: string) => tasks.find((task) => task.id === id)?.title ?? id;
  const pending = tasks.filter((task) => !terminalTask(task.state));
  const requirements = (snapshot.requirements ?? []).filter((requirement) => {
    const linked = tasks.filter((task) => task.requirement_ids?.includes(requirement.id));
    return !linked.length || linked.some((task) => !terminalTask(task.state));
  });
  const pools: { title: string; page: Page; entries: Entry[]; empty: string }[] = [
    { title: '需求池', page: 'requirements', empty: '暂无活动需求', entries: requirements.map((requirement) => ({ id: requirement.id, title: requirement.title, detail: `来源：${snapshot.nodes?.find((node) => node.id === requirement.source_node_id)?.name || (requirement.source_node_id ? '节点来源' : requirement.source ? '聊天' : '中心记录')}`, status: tasks.some((task) => task.requirement_ids?.includes(requirement.id)) ? '推进中' : '待规划', page: 'requirements', target: requirement.id })) },
    { title: '任务池', page: 'tasks', empty: '暂无活动任务', entries: pending.map((task) => {
      const run = live.find((item) => item.id === task.current_run_id);
      const node = snapshot.nodes?.find((item) => item.id === run?.node_id);
      return { id: task.id, title: task.title, detail: task.hold_reason || (run ? `执行主机：${node?.name ?? run.node_id}` : '尚未分配运行实例'), status: <Status value={task.state} />, page: 'tasks', target: task.id };
    }) },
    { title: '审查池', page: 'candidates', empty: '暂无活动候选', entries: candidates.map((candidate) => ({ id: candidate.id, title: taskTitle(candidate.task_id), detail: `固定版本 ${candidate.commit.slice(0, 12)}`, status: candidateLabels[candidate.state], page: 'candidates', target: candidate.id })) },
    { title: '阻断池', page: 'blocks', empty: '暂无阻断', entries: blocks.map((block) => ({ id: block.id, title: block.task?.title || block.target, detail: block.reason, status: block.decision ? '待决策' : block.source, page: block.page, target: block.target })) },
  ];
  const todos: Entry[] = [];
  if (snapshot.initialization && snapshot.initialization.status !== 'initialized') todos.push({ id: 'initialization', title: '完成项目初始化', detail: '核对基线与初始化状态', status: '待处理', page: 'initialization', target: '' });
  for (const task of tasks.filter((task) => task.state === 'draft')) todos.push({ id: `approve:${task.id}`, title: task.title, detail: '核对任务契约后批准执行', status: '待批准', page: 'tasks', target: task.id });
  // Block records already deduplicate server decisions and their source holds.
  for (const block of blocks.filter((block) => !!block.decision || block.id.startsWith('run:') || block.id.startsWith('candidate:') && candidates.some((candidate) => candidate.id === block.target && candidate.state === 'ready_to_land' && protectedCandidate(snapshot, candidate)))) {
    todos.push({ id: block.id, title: block.task?.title || block.target, detail: block.reason, status: block.decision ? '待决策' : '待人工核验', page: block.decision ? 'blocks' : block.page, target: block.decision ? block.decision.id : block.target });
  }
  const hosts = [...(snapshot.nodes ?? []).map((node) => ({ id: node.id, node })), ...[...new Set(live.map((run) => run.node_id))].filter((id) => !snapshot.nodes?.some((node) => node.id === id)).map((id) => ({ id, node: undefined }))];
  function link(page: Page, label: string, id?: string) {
    return <a href={`#${page}${id ? `?id=${encodeURIComponent(id)}` : ''}`} onClick={(event) => { event.preventDefault(); navigate(page, id); }}>{label}</a>;
  }
  function row(entry: Entry) {
    return <li key={entry.id}><button className="overview-row" onClick={() => navigate(entry.page, entry.target)}><span className="overview-copy"><strong>{entry.title}</strong><small>{entry.detail}</small></span><span className="overview-state">{entry.status}</span></button></li>;
  }
  const completed = tasks.filter((task) => completedTask(task.state)).length;
  const cancelled = tasks.filter((task) => task.state === 'cancelled').length;
  return <div className="overview">
    <header className="overview-header"><div><h1>{project.name}</h1><p className={`overview-objective ${expandedGoal ? 'expanded' : ''}`}>{snapshot.planning_document?.objective || '尚未记录目标'}</p>{snapshot.planning_document?.objective && <button className="text-button" aria-expanded={expandedGoal} onClick={() => setExpandedGoal(!expandedGoal)}>{expandedGoal ? '收起目标' : '展开目标'}</button>}<div className="overview-progress"><Status value={project.paused ? 'paused' : 'ready'} /><span>任务进度：{completed} / {tasks.length - cancelled} 已完成（集成或验证）；取消 {cancelled} 项不计入</span></div></div><div className="overview-header-actions">{link('requirements', '提交需求')}{link('planning', '查看目标')}</div></header>
    {todos.length > 0 && <section className="overview-todos" aria-label="人工待办"><h2>需要你处理 <span>{todos.length}</span></h2><ul>{todos.map(row)}</ul></section>}
    <div className="overview-workspace"><div className="overview-pools">{pools.map((pool) => <section className="overview-pool" key={pool.page} aria-label={pool.title}><header><h2>{pool.title} <span aria-label={`${pool.title}活动总数`}>{pool.entries.length}</span></h2>{link(pool.page, '查看全部')}</header>{pool.entries.length ? <ul>{pool.entries.slice(0, 5).map(row)}</ul> : <p className="overview-empty">{pool.empty}</p>}</section>)}</div>
      <aside className="overview-hosts" aria-label="主机运行动态"><header><h2>主机运行动态</h2>{link('agents', '管理主机')}</header>{hosts.length ? hosts.map(({ id, node }) => {
        const work = live.filter((run) => run.node_id === id);
        return <section className="overview-host" key={id} aria-label={id === 'center' ? '中心程序执行' : node?.name ?? id}><h3>{id === 'center' ? '中心程序执行' : node?.name ?? id}</h3><p>{node?.revoked ? '已撤销' : work.some((run) => run.state === 'lost') ? '存在失联实例' : work.length ? '有活动运行记录' : '空闲 · 无活动运行记录'}</p><small>{node ? `${platformLabel(node.platform)} · 最后上报 ${time(node.last_seen)} · 在线状态以实际连接为准` : id === 'center' ? '中心本机的程序验证执行器' : '主机记录未包含在快照中'}</small>{work.length ? <ul>{work.map((run) => <li key={run.id}><button className="overview-run" onClick={() => navigate('runs', run.id)}><strong>{taskTitle(run.task_id)}</strong><span>{run.tool} · {roles[run.kind] ?? run.kind} · {duration(run.elapsed_seconds)}</span><span>{run.state === 'lost' ? '失联 · 未确认静止' : <Status value={run.state} />}</span></button></li>)}</ul> : <p className="overview-empty">暂无运行实例</p>}</section>;
      }) : <p className="overview-empty">尚无主机或运行记录</p>}</aside>
    </div>
    <nav className="overview-tools" aria-label="项目管理入口">{link('initialization', '初始化')}{link('planning', 'Leader 规划与咨询')}{link('automation', '自动化')}{link('runs', '运行与恢复')}{link('audit', '审计历史')}{link('settings', '项目设置')}</nav>
  </div>;
}
