import { useState } from 'react';
import type { Decision, DecisionResolveRequest } from '../api.generated';
import { ApiClient } from '../api/client';
import { Feedback, Field } from '../components/ui';
import { useAction } from '../hooks/useAction';
export function DecisionResolve({ decision, client, refresh }: { decision: Decision; client: ApiClient; refresh: () => void }) {
  const [basis, setBasis] = useState(decision);
  const [selected, setSelected] = useState('');
  const [evidence, setEvidence] = useState('');
  const [error, setError] = useState('');
  const action = useAction(refresh);
  const stopAction = selected === 'confirm_stopped' || selected === 'confirm_run_stopped';
  const choiceAction = ['evaluate', 'edit_goal', 'edit_budget', 'dismiss'].includes(selected);
  return <form className="decision-resolve" onSubmit={(e) => { e.preventDefault(); if (basis.version !== decision.version || basis.source_version !== decision.source_version) { setError('决策或来源版本已变化，请载入当前决策并重新核对。'); return; } if (!(basis.actions ?? []).includes(selected)) { setError('请选择中心提供的操作。'); return; } if (!evidence.trim()) { setError(stopAction ? '确认停止必须提供真实、非空的进程身份及进程树核验依据。' : '所有决策操作都必须提供非空的处理依据。'); return; } setError(''); void action.execute(() => client.write(`/decisions/${encodeURIComponent(basis.id)}/resolve`, { expected_version: basis.version, expected_source_version: basis.source_version, action: selected, evidence } satisfies DecisionResolveRequest), choiceAction ? '决策选择已记录；目标编辑、目标授权或预算调整仍须按需在来源对象执行，以中心状态为准。' : '决策处理请求已受理；此回执不代表当前运行已停止，以中心状态为准。'); }}><p>决策 v{basis.version} · 来源 {basis.source_kind}/{basis.source_id} v{basis.source_version} · 文档 v{basis.document_version} / 授权 v{basis.authorization_version}</p><Field label={`决策操作 ${basis.id}`}><select value={selected} onChange={(e) => setSelected(e.target.value)}><option value="">选择操作</option>{(basis.actions ?? []).map((value) => <option key={value}>{value}</option>)}</select></Field>{stopAction && <p className="hold">Owner 必须亲自核验原始进程身份及进程树确已停止，并核对来源 Run 的版本。失联、取消请求和模型文字都不是停止证据。{selected === 'confirm_run_stopped' && '必须先在原始 Run 通过共享停止证据 API 成功记录停止证明；本决策的选项和证据文字不能替代该操作。'}</p>}{choiceAction && <p className="hold">evaluate、edit_goal、edit_budget、dismiss 主要记录决策选择，本身不会编辑或授权目标，也不会改变预算；仍须按需在来源对象执行相应操作。</p>}<Field label={`决策证据 ${basis.id}`}><textarea value={evidence} onChange={(e) => setEvidence(e.target.value)} /></Field><Feedback {...action} error={error || action.error} /><button className="secondary" disabled={action.busy}>提交决策处理</button><button type="button" className="text-button" onClick={refresh}>刷新决策版本</button><button type="button" className="text-button" onClick={() => { setBasis(decision); setSelected(''); setEvidence(''); setError(''); }}>载入当前决策</button></form>;
}
