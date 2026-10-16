import { useRouteField } from '../hooks/useRouteField';
import type { Snapshot } from '../api.generated';
import type { Page } from '../App';
import { ApiClient } from '../api/client';
import { Empty, SectionTitle, Status, roleLabel, platformLabel, duration, time } from '../components/ui';
import { NodeManagement, RevokeNode } from './NodeManagement';
import { NodeConfigure } from './NodeConfigure';
import { AgentSettings } from './AgentSettings';
import { agentRows, currentWork, matches } from './management';
import { PoolInspection } from '../components/material/PoolInspection';

export function Agents({ snapshot, client, refresh, navigate }: { snapshot: Snapshot; client: ApiClient; refresh: () => void; navigate: (page: Page, id?: string) => void }) {
  const [query, setQuery] = useRouteField('agents', 'query', '');
  const [nodeFilter, setNodeFilter] = useRouteField('agents', 'nodeFilter', 'all');
  const [tool, setTool] = useRouteField('agents', 'tool', 'all');
  const [availability, setAvailability] = useRouteField('agents', 'availability', 'all');
  const [selected, setSelected] = useRouteField('agents', 'id');
  const nodes = snapshot.nodes ?? [];
  const rows = agentRows(snapshot);
  const tools = [...new Set(rows.map((row) => row.toolId).filter(Boolean))];
  const shown = rows.filter(({ node, capability, toolId }) => (nodeFilter === 'all' || node.id === nodeFilter) && (tool === 'all' || toolId === tool) && (availability === 'all' || (availability === 'revoked' ? node.revoked : availability === 'available' ? !node.revoked && capability?.available : !node.revoked && (availability === 'unknown' ? !capability : capability?.available === false))) && matches(query, [node.id, node.name, node.platform, toolId, capability?.protocol, capability?.version, capability?.detail]));
  const live = (snapshot.runs ?? []).filter(currentWork);
  const orphaned = live.filter((run) => !nodes.some((n) => n.id === run.node_id));
  const selection = shown.find((row) => `${row.node.id}/${row.toolId}` === selected || row.node.id === selected) ?? (selected ? undefined : shown[0]);
  const state = (node: typeof nodes[number], cap: typeof rows[number]['capability'], toolId: string) => node.revoked ? '已撤销' : cap?.available ? '版本探测通过' : cap?.installed ? '探测异常' : cap ? '未检测到' : toolId ? '工具未再报告 · 可用性未知' : '可用性未知';
  const work = (nodeId: string, toolId: string) => live.filter((run) => run.node_id === nodeId && (!toolId || run.tool === toolId));
  return <><SectionTitle title="Agent 管理" actions={<NodeManagement client={client} refresh={refresh}/>}/>
    <div className="task-toolbar"><input type="search" aria-label="搜索 Agent" value={query} onChange={(e) => setQuery(e.target.value)} placeholder="主机或 Agent"/><details className="filter-options"><summary>筛选</summary><div className="actions"><select aria-label="Agent 节点" value={nodeFilter} onChange={(e) => setNodeFilter(e.target.value)}><option value="all">全部主机</option>{nodes.map((node) => <option key={node.id} value={node.id}>{node.name}</option>)}</select><select aria-label="Agent 工具" value={tool} onChange={(e) => setTool(e.target.value)}><option value="all">全部工具</option>{tools.map((value) => <option key={value}>{value}</option>)}</select><select aria-label="Agent 可用性" value={availability} onChange={(e) => setAvailability(e.target.value)}><option value="all">全部可用性</option><option value="available">探测可用 · 未撤销</option><option value="unavailable">不可用</option><option value="unknown">未知 / 未报告</option><option value="revoked">已撤销</option></select></div></details><span className="count">{new Set(shown.map((row) => row.node.id)).size} 台主机 · {live.length} 项活动记录</span></div>
    <div className={`agent-layout ${selection ? 'has-selection' : ''}`}><section className="panel agent-directory" aria-label="主机与 Agent">{shown.length ? <div className="table-scroll"><table className="agent-directory-table"><thead><tr><th>主机</th><th>Agent</th><th>接到的任务</th></tr></thead><tbody>{nodes.flatMap((node) => shown.filter((row) => row.node.id === node.id).map(({ capability, toolId }, index, hostRows) => <tr key={`${node.id}/${toolId}`}>
      {index === 0 && <td rowSpan={hostRows.length} className="agent-host-cell"><strong>{node.name}</strong><small>{platformLabel(node.platform)}</small><small>节点活动 {live.filter((r) => r.node_id === node.id).length} / 容量 {node.capacity}</small><small>最后上报 {time(node.last_seen)}</small></td>}
      <td><button className="agent-pick" aria-pressed={selection?.node.id === node.id && selection?.toolId === toolId} onClick={() => setSelected(`${node.id}/${toolId}`)}><strong>{toolId || (node.capabilities?.length ? '未发现已安装工具' : '未报告工具')}</strong><span>{state(node, capability, toolId)}</span></button></td>
      <td className="agent-work-cell">{work(node.id, toolId).length ? work(node.id, toolId).map((run) => <button key={run.id} className="agent-work" onClick={() => navigate('runs', run.id)}><strong>{snapshot.tasks?.find((t) => t.id === run.task_id)?.title ?? '未命名任务'}</strong><small>{roleLabel(run.kind)} · {run.state === 'lost' ? '失联 · 未确认静止' : <Status value={run.state}/>} · {duration(run.elapsed_seconds)}</small>{(run.model || run.reasoning_effort) && <small>{run.model} {run.reasoning_effort}</small>}</button>) : <span className="muted">暂无活动任务</span>}</td>
    </tr>))}</tbody></table></div> : <Empty title={nodes.length ? '没有匹配 Agent' : '尚无 Agent 节点'}>配对主机后显示实际工具报告。</Empty>}</section>
    {selection && <PoolInspection title={`${selection.toolId || selection.node.name} 配置`}><section className="panel agent-editor"><div className="subheading"><h2>{selection.toolId || '主机配置'}</h2><button className="text-button" onClick={() => setSelected('none')}>取消选择</button></div><p className="muted">{selection.node.name}</p>{selection.toolId && <AgentSettings key={`${selection.node.id}/${selection.toolId}`} node={selection.node} capability={selection.capability} tool={selection.toolId} client={client} refresh={refresh}/>}
      <details><summary>主机设置</summary><p className="muted">排空、禁用或撤销不表示进程已经停止。</p><NodeConfigure node={selection.node} client={client} refresh={refresh}/><RevokeNode node={selection.node} client={client} refresh={refresh}/></details>
      <details><summary>工具能力与技术详情</summary><p><span>执行认证未提供</span>。版本探测通过不代表执行认证；最后上报时间不代表在线。</p><code>{selection.node.id}</code><p>{selection.capability?.protocol || '协议未报告'} · {selection.capability?.version || '版本未知'}</p>{selection.capability ? <><p>结构化 {selection.capability.structured ? '是' : '否'} · 取消 {selection.capability.cancel ? '是' : '否'} · 评审 {selection.capability.review ? '是' : '否'}</p><p>只读 {selection.capability.read_only ? '是' : '否'} · 恢复 {selection.capability.resume ? '是' : '否'}</p><p>{selection.capability.detail}</p></> : <p>尚无能力报告</p>}</details>
    </section></PoolInspection>}{selected && selected !== 'none' && !selection && <PoolInspection title="Agent 不在当前列表"><section className="panel"><Empty title="Agent 不在当前列表">调整筛选，或选择其他 Agent。</Empty></section></PoolInspection>}</div>
    {!!orphaned.length && <section className="panel"><h2>{orphaned.every((r) => r.node_id === 'center') ? '中心程序执行' : '主机信息缺失的活动记录'}</h2>{orphaned.map((run) => <button className="text-button work-link" key={run.id} onClick={() => navigate('runs', run.id)}>{snapshot.tasks?.find((t) => t.id === run.task_id)?.title ?? '未命名任务'} · {run.state === 'lost' ? '失联 · 未确认静止' : run.state}</button>)}</section>}
  </>;
}
