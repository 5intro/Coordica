import { useState } from 'react';
import type { Node, NodeConfigureRequest } from '../api.generated';
import { ApiClient } from '../api/client';
import { Feedback, Field } from '../components/ui';
import { useAction } from '../hooks/useAction';
export function NodeConfigure({ node, client, refresh }: { node: Node; client: ApiClient; refresh: () => void }) {
  const [draft, setDraft] = useState<NodeConfigureRequest>();
  const [error, setError] = useState('');
  const action = useAction(refresh);
  function open() {
    const tools = [...new Set([...(node.capabilities ?? []).map((c) => c.tool), ...(node.tools ?? []).map((t) => t.tool)])].map((tool) => (node.tools ?? []).find((t) => t.tool === tool) ?? { tool, enabled: false, implement: false, review: false });
    setDraft({ expected_version: node.version, name: node.name, capacity: node.capacity, draining: node.draining, disabled_new_work: node.disabled_new_work, tools });
  }
  return <><button className="text-button" onClick={open}>配置调度权限</button>{draft && <form className="panel node-config" onSubmit={(e) => { e.preventDefault(); if (draft.expected_version !== node.version) { setError('节点版本已变化，请重新打开配置并核对。'); return; } if (!draft.name.trim() || ![1, 2].includes(draft.capacity)) { setError('名称不能为空，容量必须为 1 或 2。'); return; } setError(''); void action.execute(() => client.write(`/nodes/${encodeURIComponent(node.id)}/configure`, draft), '节点配置请求已完成。').then((ok) => { if (ok) setDraft(undefined); }); }}><h3>{node.name} · v{draft.expected_version}</h3><Feedback {...action} error={error || action.error} /><button type="button" className="text-button" onClick={refresh}>刷新节点版本</button><p>能力事实来自节点报告；以下是 Owner 调度权限。排空、禁用新工作或撤销均不表示进程停止。</p><Field label="节点名称"><input value={draft.name} onChange={(e) => setDraft({ ...draft, name: e.target.value })} /></Field><Field label="节点容量"><input type="number" value={draft.capacity} onChange={(e) => setDraft({ ...draft, capacity: e.target.valueAsNumber })} /></Field>{(['draining', 'disabled_new_work'] as const).map((key) => <Field key={key} label={key === 'draining' ? '排空' : '禁用新工作'}><input type="checkbox" checked={draft[key]} onChange={(e) => setDraft({ ...draft, [key]: e.target.checked })} /></Field>)}{(draft.tools ?? []).map((tool) => <fieldset key={tool.tool}><legend>{tool.tool} · {(node.capabilities ?? []).some((c) => c.tool === tool.tool) ? '节点已报告' : '工具未再报告'}</legend>{(['enabled', 'implement', 'review'] as const).map((key) => <Field key={key} label={`${tool.tool} ${key}`}><input type="checkbox" checked={tool[key]} onChange={(e) => setDraft({ ...draft, tools: (draft.tools ?? []).map((t) => t.tool === tool.tool ? { ...t, [key]: e.target.checked } : t) })} /></Field>)}</fieldset>)}<button className="primary" disabled={action.busy}>保存节点配置</button><button type="button" className="secondary" onClick={() => setDraft(undefined)}>关闭配置</button></form>}</>;
}
