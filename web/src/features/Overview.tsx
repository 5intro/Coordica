import { reasonLabel } from '../components/ui';
import { useId, useState, type ReactNode } from 'react';
import { PoolRow } from '../components/material/PoolRow';
import { Glass } from '../components/material/Glass';
import { Icon } from '../components/material/icons';
import { DetailPanel } from '../components/material/DetailPanel';
import { WorkItemPanel, type WorkTarget } from './WorkItemPanel';
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

export function Overview({ snapshot, navigate, onNewRequirement }: { snapshot: Snapshot; navigate: (page: Page, id?: string) => void; onNewRequirement?: () => void }) {
  const [selection, setSelection] = useState<WorkTarget[]>([]);
  const [showTodos, setShowTodos] = useState(false);
  function inspect(page: Page, id: string) {
    if (['requirements', 'tasks', 'candidates', 'blocks', 'runs'].includes(page)) setSelection([{ page, id }]);
    else navigate(page, id);
  }
  const [expandedGoal, setExpandedGoal] = useState(false);
  const goalId = useId();
  const project = snapshot.project;
  if (!project) return <div className="overview"><h1>尚无项目</h1><p>等待中心提供项目快照。</p></div>;
  const tasks = snapshot.tasks ?? [];
  const runs = snapshot.runs ?? [];
  const live = runs.filter(currentWork);
  const candidates = (snapshot.candidates ?? []).filter((candidate) => !!candidate.commit && ['reviewing', 'ready_to_land'].includes(candidate.state));
  const blocks = blockRecords(snapshot);
  const taskTitle = (id: string) => tasks.find((task) => task.id === id)?.title ?? '未命名任务';
  const pending = tasks.filter((task) => !terminalTask(task.state));
  const requirements = (snapshot.requirements ?? []).filter((requirement) => {
    const linked = tasks.filter((task) => task.requirement_ids?.includes(requirement.id));
    return !linked.length || linked.some((task) => !terminalTask(task.state));
  });
  const pools: { title: string; page: Page; entries: Entry[]; empty: string }[] = [
    { title: '需求池', page: 'requirements', empty: '暂无活动需求', entries: requirements.map((requirement) => ({ id: requirement.id, title: requirement.title, detail: `${snapshot.nodes?.find((node) => node.id === requirement.source_node_id)?.name || (requirement.source_node_id ? '节点来源' : requirement.source ? '聊天' : '中心记录')}`, status: tasks.some((task) => task.requirement_ids?.includes(requirement.id)) ? '推进中' : '待规划', page: 'requirements', target: requirement.id })) },
    { title: '任务池', page: 'tasks', empty: '暂无活动任务', entries: pending.map((task) => {
      const run = live.find((item) => item.id === task.current_run_id);
      const node = snapshot.nodes?.find((item) => item.id === run?.node_id);
      return { id: task.id, title: task.title, detail: (task.hold_reason ? `阻断原因：${reasonLabel(task.hold_reason)}` : '') || (run ? `执行主机：${node?.name ?? '主机未命名'}` : '尚未分配运行实例'), status: <Status value={task.state} />, page: 'tasks', target: task.id };
    }) },
    { title: '审查池', page: 'candidates', empty: '暂无活动候选', entries: candidates.map((candidate) => ({ id: candidate.id, title: taskTitle(candidate.task_id), detail: `固定版本 ${candidate.commit.slice(0, 12)}`, status: candidateLabels[candidate.state], page: 'candidates', target: candidate.id })) },
    { title: '阻断池', page: 'blocks', empty: '暂无阻断', entries: blocks.map((block) => ({ id: block.id, title: block.task?.title || reasonLabel(block.reason), detail: reasonLabel(block.reason), status: block.decision ? '待决策' : reasonLabel(block.source), page: block.page, target: block.target })) },
  ];
  const todos: Entry[] = [];
  if (snapshot.initialization && snapshot.initialization.status !== 'initialized') todos.push({ id: 'initialization', title: '完成项目初始化', detail: '核对基线与初始化状态', status: '待处理', page: 'initialization', target: '' });
  for (const task of tasks.filter((task) => task.state === 'draft')) todos.push({ id: `approve:${task.id}`, title: task.title, detail: '核对任务契约后批准执行', status: '待批准', page: 'tasks', target: task.id });
  // Block records already deduplicate server decisions and their source holds.
  for (const block of blocks.filter((block) => !!block.decision || block.id.startsWith('run:') || block.id.startsWith('candidate:') && candidates.some((candidate) => candidate.id === block.target && candidate.state === 'ready_to_land' && protectedCandidate(snapshot, candidate)))) {
    todos.push({ id: block.id, title: block.task?.title || reasonLabel(block.reason), detail: reasonLabel(block.reason), status: block.decision ? '待决策' : '待人工核验', page: block.decision ? 'blocks' : block.page, target: block.decision ? block.decision.id : block.target });
  }
  const hosts = [...(snapshot.nodes ?? []).map((node) => ({ id: node.id, node })), ...[...new Set(live.map((run) => run.node_id))].filter((id) => !snapshot.nodes?.some((node) => node.id === id)).map((id) => ({ id, node: undefined }))];
  function link(page: Page, label: string, id?: string) {
    return <a href={`#${page}${id ? `?id=${encodeURIComponent(id)}` : ''}`} onClick={(event) => { event.preventDefault(); navigate(page, id); }}>{label}</a>;
  }
  function row(entry: Entry) {
    return <li key={entry.id}><Glass className="work-card" radius={14}><button className="overview-row" onClick={() => inspect(entry.page, entry.target)}><strong>{entry.title}</strong><span className="overview-row-meta"><span className="overview-state">{entry.status}</span><small>{entry.detail}</small></span></button></Glass></li>;
  }
  const completed = tasks.filter((task) => completedTask(task.state)).length;
  const cancelled = tasks.filter((task) => task.state === 'cancelled').length;
  return <div className="overview">
    <header className="overview-header"><div><h1>{project.name}</h1><div className="overview-progress"><Status value={project.paused ? 'paused' : 'ready'} /><span>{completed}/{tasks.length - cancelled} 已完成</span>{snapshot.planning_document?.objective && <button className="text-button" aria-expanded={expandedGoal} aria-controls={goalId} onClick={() => setExpandedGoal(!expandedGoal)}>{expandedGoal ? '收起目标' : '展开目标'}</button>}</div><p id={goalId} hidden={!expandedGoal} className="overview-objective">{snapshot.planning_document?.objective || '尚未记录目标'}</p></div><div className="overview-header-actions"><a href="#requirements" aria-label="提交需求" onClick={(event) => { event.preventDefault(); if (onNewRequirement) onNewRequirement(); else navigate('requirements', undefined); }}><Icon name="plus" size={16}/>新需求</a></div></header>
    {todos.length > 0 && <section className="overview-todos" aria-label="人工待办"><div className="overview-todo-heading"><Icon name="block" size={23}/><h2>需要你处理 <span>{todos.length}</span></h2>{todos.length > 1 && <button className="text-button" onClick={() => setShowTodos(true)}>查看全部</button>}</div><ul>{todos.slice(0,1).map(row)}</ul></section>}
    <div className="overview-workspace"><div className="overview-pools">{[pools.slice(0,2),pools.slice(2)].map((pair,rowIndex) => <PoolRow row={rowIndex} count={Math.max(...pair.map((pool) => pool.entries.length))} key={rowIndex}>{pair.map((pool) => <section className="overview-pool-group" key={pool.page} aria-label={pool.title}><header><h2>{link(pool.page,pool.title)} <span aria-label={`${pool.title}活动总数`}>{pool.entries.length}</span></h2></header><div className="overview-pool pool-well">{pool.entries.length ? <ul>{pool.entries.map(row)}</ul> : <p className="overview-empty">{pool.empty}</p>}</div></section>)}</PoolRow>)}</div>
      <aside className="overview-hosts" aria-label="主机运行动态"><header><h2>此刻的协作</h2>{link('agents', '管理主机')}</header>{hosts.length ? hosts.slice(0,3).map(({ id, node }) => {
        const work = live.filter((run) => run.node_id === id);
        return <section className="overview-host" key={id} aria-label={id === 'center' ? '中心程序执行' : node?.name ?? '主机未命名'}><div className="overview-host-title"><h3>{id === 'center' ? '中心程序执行' : node?.name ?? '主机未命名'}</h3><span>{node?.revoked ? '已撤销' : work.some((run) => run.state === 'lost') ? '存在失联实例' : work.length ? `${work.length} 项活动` : '空闲 · 无活动运行记录'}</span></div><small>{node ? `${platformLabel(node.platform)} · 最后上报 ${time(node.last_seen)}` : id === 'center' ? '程序验证执行器' : '主机信息暂缺'}</small>{work.length ? <ul>{work.slice(0,2).map((run) => <li key={run.id}><button className="overview-run" onClick={() => inspect('runs', run.id)}><span className="overview-run-tool"><span className="overview-avatar" aria-hidden="true">{run.tool.slice(0,1).toUpperCase()}</span><strong>{run.tool}</strong><small>{roles[run.kind] ?? run.kind}</small></span><strong>{taskTitle(run.task_id)}</strong><span>{run.state === 'lost' ? '失联 · 未确认静止' : <Status value={run.state} />}<small>{duration(run.elapsed_seconds)}</small></span></button></li>)}</ul> : <p className="overview-empty">暂无运行实例</p>}{work.length > 2 && link('agents', `查看其余 ${work.length - 2} 项活动`)}</section>;
      }) : <p className="overview-empty">尚无主机或运行记录</p>}{hosts.length > 3 && link('agents', `查看全部 ${hosts.length} 台主机`)}<div className="overview-host-footer">{live.length} 个活动实例 · {hosts.length} 台主机</div></aside>
    </div>
    <div className="overview-bottom"><span>{completed} 项工作已完成</span>{link('testing', '查看测试概况 →')}</div>
    <details className="overview-management"><summary>项目管理</summary><nav className="overview-tools" aria-label="项目管理入口">{link('initialization', '初始化')}{link('planning', 'Leader 规划与咨询')}{link('automation', '自动化')}{link('runs', '运行与恢复')}{link('audit', '审计历史')}{link('settings', '项目设置')}</nav></details>
    {selection.length > 0 && <WorkItemPanel snapshot={snapshot} target={selection.at(-1)!} onClose={() => setSelection([])} open={(target) => setSelection((previous) => [...previous, target])} back={selection.length > 1 ? () => setSelection((previous) => previous.slice(0,-1)) : undefined} navigate={(page,id) => { setSelection([]); navigate(page,id); }}/>}
    {showTodos && <DetailPanel title="需要你处理" onClose={() => setShowTodos(false)}><h2>需要你处理</h2><ul className="overview-todo-list">{todos.map((entry) => <li key={entry.id}><Glass className="work-card" radius={14}><button onClick={() => { setShowTodos(false); navigate(entry.page,entry.target); }}><strong>{entry.title}</strong><span>{entry.status}</span><small>{entry.detail}</small></button></Glass></li>)}</ul></DetailPanel>}
  </div>;
}
