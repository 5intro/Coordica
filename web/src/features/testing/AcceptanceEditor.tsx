import { useState } from 'react';
import type { FormEvent } from 'react';
import type { AcceptancePlan, Requirement } from '../../api.generated';
import { ApiClient } from '../../api/client';
import { checked, isAcceptancePlan } from '../../api/testCatalog';
import { Feedback, Field } from '../../components/ui';
import { useAction } from '../../hooks/useAction';

type Props = { plan: AcceptancePlan; requirement: Requirement; client: ApiClient; onSaved: () => void };
export function AcceptanceEditor({ plan, requirement, client, onSaved }: Props) {
  const [criteria, setCriteria] = useState(plan.criteria ?? []);
  const action = useAction(() => {});
  const change = (index: number, value: object) => setCriteria((items) => items.map((v, i) => i === index ? { ...v, ...value } : v));
  async function save(event: FormEvent) {
    event.preventDefault();
    await action.execute(async () => {
      const next = { ...plan, requirement_id: requirement.id, revision: requirement.content_revision || 1, criteria };
      const value = checked(await client.write<unknown>('/tests/acceptance', {
        expected_requirement_version: requirement.version, expected_version: plan.version, plan: next,
      }), isAcceptancePlan);
      if (value.requirement_id !== requirement.id || value.version !== plan.version + 1) throw new Error('验收回执版本不一致，请刷新核对。');
      onSaved();
    }, '验收条款已保存。');
  }
  return <form className="panel testing-form" onSubmit={save} aria-label="编辑验收条款">
    <h2>验收条款</h2>
    {criteria.map((criterion, index) => <div className="acceptance-edit-row" key={criterion.id}>
      <Field label={`条款 ${index + 1}`}><textarea required rows={2} value={criterion.title}
        onChange={(e) => change(index, { title: e.target.value })} /></Field>
      <Field label={`条款 ${index + 1} 验证方式`}><select value={criterion.method} onChange={(e) => change(index, { method: e.target.value })}>
        <option value="automated">自动测试</option><option value="manual">人工验收</option>
      </select></Field>
      <button type="button" className="text-button" aria-label={`移除条款 ${index + 1}`}
        onClick={() => setCriteria((items) => items.filter((_, i) => i !== index))}>移除</button>
    </div>)}
    <button type="button" className="secondary" disabled={criteria.length >= 32} onClick={() => setCriteria((items) => [...items, {
      id: `criterion_${crypto.randomUUID().replaceAll('-', '')}`, title: '', method: 'automated',
    }])}>添加条款</button>
    {!!plan.version && <small>保存新版本后，现有测试需重新关联并执行。</small>}
    <Feedback error={action.error} notice={action.notice} />
    <button className="primary" disabled={action.busy || !criteria.length}>{action.busy ? '正在保存…' : '保存验收条款'}</button>
  </form>;
}
