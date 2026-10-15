import { useRouteField } from '../hooks/useRouteField';
import type { Snapshot } from '../api.generated';
import type { Page } from '../App';
import { ApiClient } from '../api/client';
import { Empty, SectionTitle, Status, roleLabel, platformLabel, duration, time } from '../components/ui';
import { NodeManagement, RevokeNode } from './NodeManagement';
import { NodeConfigure } from './NodeConfigure';
import { agentRows, currentWork, matches } from './management';
export function Agents({ snapshot, client, refresh, navigate }: { snapshot: Snapshot; client: ApiClient; refresh: () => void; navigate: (page: Page, id?: string) => void }) {
  const [query, setQuery] = useRouteField('agents', 'query', '');
  const [nodeFilter, setNodeFilter] = useRouteField('agents', 'nodeFilter', 'all');
  const [tool, setTool] = useRouteField('agents', 'tool', 'all');
  const [availability, setAvailability] = useRouteField('agents', 'availability', 'all');
  const nodes = snapshot.nodes ?? [];
  const rows = agentRows(snapshot);
  const tools = [...new Set(rows.map((row) => row.toolId).filter(Boolean))];
  const shown = rows.filter(({ node, capability, toolId }) => (nodeFilter === 'all' || node.id === nodeFilter) && (tool === 'all' || toolId === tool) && (availability === 'all' || (availability === 'revoked' ? node.revoked : availability === 'available' ? !node.revoked && capability?.available : !node.revoked && (availability === 'unknown' ? !capability : capability?.available === false))) && matches(query, [node.id, node.name, node.platform, toolId, capability?.protocol, capability?.version, capability?.detail]));
  const live = (snapshot.runs ?? []).filter(currentWork);
  const hostIds = [...new Set([...nodes.map((node) => node.id), ...live.map((run) => run.node_id)])];
  return <><SectionTitle eyebrow="AGENTS" title="Agent 管理" description="按主机展示实际运行。版本探测通过不代表执行认证；最后上报时间不代表在线。撤销不代表进程已停止。" />
    <section className="panel"><h2>实际运行实例</h2><p>开发模型固定 gpt-6.1-sol；思考强度无可写 API。</p>
      <details><summary>调度配置</summary><p>开发 {snapshot.automation.development_tool} / {snapshot.automation.development_model}，Leader {snapshot.automation.leader_tool} / {snapshot.automation.leader_model}。</p></details>
      {!live.length && <p>暂无活动运行实例。已安装工具仅表示上报能力。</p>}
      {hostIds.filter((id) => live.some((run) => run.node_id === id)).map((id) => <div key={id}><h3>{id === 'center' ? '中心程序执行' : nodes.find((node) => node.id === id)?.name ?? id}</h3><div className="rows">{live.filter((run) => run.node_id === id).map((run) => <a className="data-row" key={run.id} href={`#runs?id=${encodeURIComponent(run.id)}`}><span><strong>{snapshot.tasks?.find((task) => task.id === run.task_id)?.title ?? run.task_id}</strong><small>{roleLabel(run.kind)} · {run.tool} · {duration(run.elapsed_seconds)}</small></span><span>{run.state === 'lost' ? '失联 · 未确认静止' : <Status value={run.state} />}</span></a>)}</div></div>)}
    </section><h2>主机与工具能力</h2><NodeManagement client={client} refresh={refresh} />
    <div className="task-toolbar"><input type="search" aria-label="搜索 Agent" value={query} onChange={(e) => setQuery(e.target.value)} placeholder="节点、工具、协议或版本" /><select aria-label="Agent 节点" value={nodeFilter} onChange={(e) => setNodeFilter(e.target.value)}><option value="all">全部节点</option>{nodes.map((node) => <option key={node.id} value={node.id}>{node.name}</option>)}</select><select aria-label="Agent 工具" value={tool} onChange={(e) => setTool(e.target.value)}><option value="all">全部工具</option>{tools.map((value) => <option key={value}>{value}</option>)}</select><select aria-label="Agent 可用性" value={availability} onChange={(e) => setAvailability(e.target.value)}><option value="all">全部可用性</option><option value="available">探测可用 · 未撤销</option><option value="unavailable">不可用</option><option value="unknown">未知 / 未报告</option><option value="revoked">已撤销</option></select><span className="count">{new Set(shown.map((row) => row.node.id)).size} 台主机</span></div>
    {nodes.filter((node) => shown.some((row) => row.node.id === node.id)).map((node) => {
      const nodeWork = live.filter((run) => run.node_id === node.id);
      return <section className="panel agent-host" key={node.id} aria-label={node.name}><div className="subheading"><h2>{node.name}</h2><strong>{node.revoked ? '已撤销' : '在线状态以实际连接为准'}</strong></div><p>{platformLabel(node.platform)} · 最后上报 {time(node.last_seen)}</p><p>节点活动 {nodeWork.length} / 容量 {node.capacity}（含未静止失联）</p>
        <div className="actions"><NodeConfigure node={node} client={client} refresh={refresh} /><RevokeNode node={node} client={client} refresh={refresh} /></div>
        <details open={!!query || tool !== 'all' || availability !== 'all'}><summary>工具能力与技术详情</summary><code>{node.id}</code><div className="table-scroll"><table><thead><tr><th>工具 / 协议 / 版本</th><th>上报能力 <small>执行认证未提供</small></th><th>运行诊断</th></tr></thead><tbody>{shown.filter((row) => row.node.id === node.id).map(({ capability, toolId }) => <tr key={toolId || 'unreported'}><td><strong>{toolId || '未报告工具'}</strong><small>{capability?.protocol || '协议未报告'} · {capability?.version || '版本未知'}</small><span>{node.revoked ? '已撤销' : capability?.available ? '版本探测通过' : capability ? '不可用' : toolId ? '工具未再报告 · 可用性未知' : '可用性未知'}</span></td><td>{capability ? <><small>结构化 {capability.structured ? '是' : '否'} · 取消 {capability.cancel ? '是' : '否'} · 评审 {capability.review ? '是' : '否'}</small><small>只读 {capability.read_only ? '是' : '否'} · 恢复 {capability.resume ? '是' : '否'}</small><small>{capability.detail || '无补充报告'}</small></> : '尚无能力报告'}</td><td>{nodeWork.filter((run) => !toolId || run.tool === toolId).map((run) => <button key={run.id} className="text-button work-link" onClick={() => navigate('runs', run.id)}>{snapshot.tasks?.find((task) => task.id === run.task_id)?.title ?? run.task_id} · {run.kind} · {run.state === 'lost' ? '失联 · 未确认静止' : run.state} →</button>)}</td></tr>)}</tbody></table></div></details>
      </section>;
    })}
    {!shown.length && <section className="panel"><Empty title={nodes.length ? '没有匹配 Agent' : '尚无 Agent 节点'}>配对节点后显示实际工具报告。</Empty></section>}
  </>;
}
