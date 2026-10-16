import { useState } from 'react';
import type { Capability, Node, NodeConfigureRequest, ToolScheduling } from '../api.generated';
import { ApiClient } from '../api/client';
import { Feedback, Field } from '../components/ui';
import { useAction } from '../hooks/useAction';

function preferences(node: Node): ToolScheduling[] {
  return [...new Set([...(node.tools ?? []).map((t) => t.tool), ...(node.capabilities ?? []).map((c) => c.tool)])].map((tool) =>
    node.tools?.find((t) => t.tool === tool) ?? { tool, enabled: !node.scheduling_configured, implement: !node.scheduling_configured, review: !node.scheduling_configured });
}

/** Edits one tool while preserving the host and all other tool permissions. */
export function AgentSettings({ node, capability, tool, client, refresh }: { node: Node; capability?: Capability; tool: string; client: ApiClient; refresh: () => void }) {
  const [draft, setDraft] = useState(() => ({ version: node.version, value: preferences(node).find((p) => p.tool === tool) ?? { tool, enabled: false, implement: false, review: false } }));
  const [error, setError] = useState('');
  const action = useAction(refresh);
  const models = capability?.model_discovery === 'reported' ? capability.models ?? [] : [];
  const codexDefault = tool === 'codex' || capability?.protocol === 'codex-jsonl';
  const model = models.find((m) => m.id === draft.value.model);
  const stale = draft.version < node.version;
  function reset() { setDraft({ version: node.version, value: preferences(node).find((p) => p.tool === tool) ?? { tool, enabled: false, implement: false, review: false } }); setError(''); }
  return <form className="agent-settings" aria-label={`${tool} 执行配置`} onSubmit={(e) => {
    e.preventDefault();
    if (stale) { setError('主机配置已变化，请重新载入后核对。'); return; }
    if (draft.version > node.version) { setError('已保存的配置正在同步，请稍后再试。'); refresh(); return; }
    const old = node.tools?.find((p) => p.tool === tool);
    const reduction = old && (old.model ?? '') === (draft.value.model ?? '') && (old.reasoning_effort ?? '') === (draft.value.reasoning_effort ?? '')
      && (!draft.value.enabled || old.enabled) && (!draft.value.implement || old.implement) && (!draft.value.review || old.review);
    if (!reduction && (draft.value.model || draft.value.reasoning_effort) && (!model || draft.value.reasoning_effort && !model.reasoning_efforts?.includes(draft.value.reasoning_effort))) { setError('模型或思考强度已不在最新能力报告中。'); return; }
    const request: NodeConfigureRequest = { expected_version: draft.version, name: node.name, capacity: node.capacity, draining: node.draining, disabled_new_work: node.disabled_new_work, tools: [...preferences(node).filter((p) => p.tool !== tool), draft.value] };
    setError('');
    void action.execute(async () => {
      const result = await client.write<Node>(`/nodes/${encodeURIComponent(node.id)}/configure`, request);
      if (result.version) setDraft({ version: result.version, value: result.tools?.find((p) => p.tool === tool) ?? draft.value });
    }, '已保存，仅用于后续新运行。');
  }}>
    <p className="muted">仅影响新运行，已派出的任务保留原参数。</p>
    <Feedback {...action} error={error || action.error}/>
    {models.length ? <>
      <Field label="执行模型"><select value={draft.value.model ?? ''} disabled={node.revoked || action.busy} onChange={(e) => setDraft({ ...draft, value: { ...draft.value, model: e.target.value, reasoning_effort: '' } })}><option value="">{codexDefault ? '项目默认 · gpt-6.1-sol' : '工具默认'}</option>{draft.value.model && !model && <option value={draft.value.model} disabled>原模型已不可用</option>}{models.map((m) => <option key={m.id} value={m.id}>{m.name || m.id}</option>)}</select></Field>
      <Field label="思考强度"><select value={draft.value.reasoning_effort ?? ''} disabled={node.revoked || action.busy || !model || !model.reasoning_efforts?.length} onChange={(e) => setDraft({ ...draft, value: { ...draft.value, reasoning_effort: e.target.value } })}><option value="">{model?.default_reasoning_effort ? `工具默认 · ${model.default_reasoning_effort}` : '工具默认'}</option>{draft.value.reasoning_effort && !model?.reasoning_efforts?.includes(draft.value.reasoning_effort) && <option value={draft.value.reasoning_effort} disabled>原强度已不可用</option>}{model?.reasoning_efforts?.map((value) => <option key={value} value={value}>{value}</option>)}</select></Field>
    </> : <><p className="muted">{capability?.model_discovery === 'unavailable' ? '模型目录暂时无法读取。' : '此工具尚未报告可配置的模型目录。'}</p>{(draft.value.model || draft.value.reasoning_effort) && <button type="button" className="text-button" onClick={() => setDraft({ ...draft, value: { ...draft.value, model: '', reasoning_effort: '' } })}>清除原执行参数</button>}</>}
    <details className="agent-permissions"><summary>调度权限</summary><fieldset disabled={node.revoked || action.busy}><legend className="sr-only">调度权限</legend>{(['enabled', 'implement', 'review'] as const).map((key) => <Field key={key} label={{ enabled: '允许调度', implement: '开发任务', review: '审查任务' }[key]}><input type="checkbox" checked={draft.value[key]} onChange={(e) => setDraft({ ...draft, value: { ...draft.value, [key]: e.target.checked } })}/></Field>)}</fieldset></details>
    {stale && <p className="hold">主机配置已更新。当前草稿保留，保存前需重新核对。</p>}
    <div className="actions"><button className="primary" disabled={node.revoked || action.busy}>保存 Agent 配置</button><button className="text-button" type="button" onClick={reset} disabled={action.busy}>重新载入</button></div>
  </form>;
}
