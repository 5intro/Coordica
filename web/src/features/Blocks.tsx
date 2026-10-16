import { reasonLabel, Empty, SectionTitle, duration, time } from '../components/ui';
import { useRouteField } from '../hooks/useRouteField';
import type { Decision, Snapshot } from '../api.generated';
import { ApiClient } from '../api/client';
import { DecisionResolve } from './DecisionResolve';
import type { Page } from '../App';
import { blockRecords, matches, sourcePage } from './management';
import { ScopeRequests } from './ScopeRequests';
import { Glass } from '../components/material/Glass';
import { PoolInspection } from '../components/material/PoolInspection';
function ScopeWaitStopNavigation({ decision, snapshot, navigate }: { decision: Decision; snapshot: Snapshot; navigate: (page: Page, id?: string) => void }) {
  const request = (snapshot.scope_requests ?? []).find((request) => request.id === decision.source_id);
  return <><p className="hold">请在原始 Run 核对进程身份及进程树，并通过运行页记录真实停止证明。</p>{request?.run_id ? <button className="text-button" onClick={() => navigate('runs', request.run_id)}>核对原始 Run / 记录停止证明</button> : <div className="muted">原始 Run 来源事实不可用：{request ? '范围请求未提供运行关联。' : '快照中缺少范围请求。'}<details><summary>技术详情：原始关联</summary><code>{decision.source_id}</code></details></div>}</>;
}
export function Blocks({ snapshot, navigate, client, refresh }: { snapshot: Snapshot; client: ApiClient; refresh: () => void; navigate: (page: Page, id?: string) => void }) {
  const [query, setQuery] = useRouteField('blocks', 'query');
  const [source, setSource] = useRouteField('blocks', 'source', 'all');
  const [selected, setSelected] = useRouteField('blocks', 'id');
  const [sort, setSort] = useRouteField('blocks', 'sort', 'source');
  const [history, setHistory] = useRouteField('blocks', 'history', 'all');
  const records = blockRecords(snapshot);
  const shown = records.filter((row) => (source === 'all' || row.source === source) && matches(query, [row.id, row.source, row.reason, row.evidence, row.task?.title, ...(row.scopeRequests ?? []).flatMap((r) => [r.id, r.reason, ...(r.paths ?? []), r.run_id, r.state])])).sort((a, b) => sort === 'reason' ? a.reason.localeCompare(b.reason, 'zh-CN') : a.source.localeCompare(b.source, 'zh-CN'));
  const past = (snapshot.decisions ?? []).filter((d) => !records.some((row) => row.decision?.id === d.id) && (source === 'all' || source === d.source_kind) && matches(query, [d.id, d.kind, d.status, d.evidence, d.resolution, d.source_id, d.source_kind])).sort((a, b) => sort === 'reason' ? a.kind.localeCompare(b.kind, 'zh-CN') : a.source_kind.localeCompare(b.source_kind, 'zh-CN'));
  const items = [...(history === 'history' ? [] : [...shown].sort((a, b) => Number(!!b.decision) - Number(!!a.decision)).map((row) => ({ id: row.id, title: reasonLabel(row.reason), subtitle: row.task?.title || reasonLabel(row.source), row, decision: undefined as Decision | undefined }))), ...(history === 'active' ? [] : past.map((d) => ({ id: `decision:${d.id}`, title: reasonLabel(d.kind), subtitle: d.status === 'resolved' ? '已处理' : d.status, row: undefined, decision: d })))];
  const choice = items.find((item) => item.id === selected || item.row?.decision?.id === selected) ?? (selected ? undefined : items[0]);
  return <><SectionTitle title="阻断池" filters={<div className="segmented" aria-label="列表范围"><button aria-pressed={history === 'all'} onClick={() => setHistory('all')}>全部</button><button aria-pressed={history === 'active'} onClick={() => setHistory('active')}>活动</button><button aria-pressed={history === 'history'} onClick={() => setHistory('history')}>历史</button></div>}/>
    <div className="task-toolbar"><input aria-label="搜索阻断" type="search" value={query} onChange={(e) => setQuery(e.target.value)} placeholder="原因或任务"/><details className="filter-options"><summary>筛选与排序</summary><div className="actions"><select aria-label="阻断来源" value={source} onChange={(e) => { setSource(e.target.value); setSelected(''); }}><option value="all">全部来源 · {records.length}</option>{[...new Set([...records.map((row) => row.source), ...(snapshot.decisions ?? []).map((d) => d.source_kind)])].map((value) => <option key={value} value={value}>{reasonLabel(value)}</option>)}</select><select aria-label="阻断排序" value={sort} onChange={(e) => setSort(e.target.value)}><option value="source">按来源</option><option value="reason">按原因</option></select><select aria-label="阻断历史" value={history} onChange={(e) => { setHistory(e.target.value); setSelected(''); }}><option value="all">活动与历史</option><option value="active">活动阻断</option><option value="history">决策处理历史</option></select></div></details></div>
    <div className="management-layout"><section className="panel collection-list" aria-label="阻断列表">{items.length ? items.map((item) => <Glass className="work-card" key={item.id} radius={14}><button className="collection-item" aria-pressed={choice?.id === item.id} onClick={() => setSelected(item.id)}><strong>{item.title}</strong><small>{item.subtitle}</small></button></Glass>) : <Empty title={records.length || past.length || query ? '没有匹配阻断' : '暂无阻断记录'}>当前没有需要展示的记录。</Empty>}</section>
    <PoolInspection title="阻断详情">{choice ? <article className="panel"><h2>{choice.title}</h2><a className="pool-detail-link" href={`#blocks?id=${encodeURIComponent(choice.id)}${choice.decision ? '&history=history' : ''}`}>阻断永久链接</a>{choice.row ? <>
      <p>{choice.row.task?.title || reasonLabel(choice.row.source)}</p><details><summary>技术详情：阻断证据</summary><code>{choice.row.id}</code><p>原因原始值：{choice.row.reason}</p><p>来源：{choice.row.source} · {choice.row.target}</p><p>{choice.row.evidence}</p></details>
      {!!choice.row.scopeRequests?.length && <ScopeRequests snapshot={snapshot} requests={choice.row.scopeRequests} navigate={navigate}/>}
      {choice.row.task && <details><summary>执行用量</summary><p>已用 {duration(choice.row.task.used_seconds)} · 预留 {duration(choice.row.task.reserved_seconds)} / {duration(choice.row.task.budget_seconds)}</p></details>}
      {choice.row.decision?.kind === 'scope_wait_stop' ? <ScopeWaitStopNavigation decision={choice.row.decision} snapshot={snapshot} navigate={navigate}/> : <button className="text-button" onClick={() => navigate(choice.row!.page, choice.row!.target)}>{choice.row.next} →</button>}
      {choice.row.decision?.resolution && <p>处理：{choice.row.decision.resolution}</p>}{choice.row.decision && choice.row.decision.kind !== 'scope_wait_stop' && <DecisionResolve key={choice.row.decision.id} decision={choice.row.decision} client={client} refresh={refresh}/>}
    </> : choice.decision && <><p>{choice.decision.evidence}</p><p>处理：{choice.decision.resolution || '尚无处理记录'}</p><details><summary>技术详情：来源</summary><code>{choice.decision.id}</code><p>{choice.decision.source_kind}/{choice.decision.source_id} v{choice.decision.source_version}</p></details>{choice.decision.kind === 'scope_wait_stop' ? <ScopeWaitStopNavigation decision={choice.decision} snapshot={snapshot} navigate={navigate}/> : <button className="text-button" onClick={() => navigate(sourcePage(choice.decision!.source_kind), choice.decision!.source_id)}>查看来源 →</button>}{(snapshot.events ?? []).filter((event) => event.object_id === choice.decision!.id).map((event) => <p key={event.sequence}>{time(event.time)} · {event.kind} · {event.detail}</p>)}</>}</article> : <section className="panel"><Empty title={selected ? '阻断不在当前列表' : '选择一项阻断'}>查看原因与可执行操作。</Empty></section>}</PoolInspection></div>
  </>;
}
