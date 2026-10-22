import { useEffect, useState } from 'react';
import type { AcceptancePlan, Snapshot, TestCase } from '../../api.generated';
import { ApiClient, messageOf } from '../../api/client';
import { checked, isAcceptancePlan } from '../../api/testCatalog';
import { Feedback, Field } from '../../components/ui';

type Props = { draft: TestCase; snapshot: Snapshot; client: ApiClient; change: (v: Partial<TestCase>) => void; active?: boolean };
export function CaseAcceptanceLinks({ draft, snapshot, client, change, active = true }: Props) {
  const [id, setID] = useState('');
  const [plan, setPlan] = useState<AcceptancePlan>();
  const [error, setError] = useState('');
  const requirement = snapshot.requirements?.find((r) => r.id === id);
  useEffect(() => {
    setPlan(undefined); setError('');
    if (!id || !active) return;
    const controller = new AbortController();
    client.read<unknown>(`/tests/acceptance/plan?requirement_id=${encodeURIComponent(id)}`, controller.signal)
      .then((raw) => { const v = checked(raw, isAcceptancePlan); if (!controller.signal.aborted) setPlan(v); })
      .catch((e) => { if (!controller.signal.aborted) setError(messageOf(e)); });
    return () => controller.abort();
  }, [client, id, active, requirement?.content_revision, requirement?.state]);
  const current = requirement && requirement.state !== 'withdrawn' && plan &&
    plan.revision === (requirement.content_revision || 1) && plan.version > 0;
  return <fieldset><legend>需求验收关联</legend>
    <ul className="catalog-links">{draft.coverage?.map((ref, index) => <li key={`${ref.requirement_id}:${ref.criterion_id}`}>
      <span>{snapshot.requirements?.find((r) => r.id === ref.requirement_id)?.title ?? '历史需求'} · 条款关联 {index + 1}
        <small>需求修订 {ref.revision} · 验收 v{ref.plan_version}</small></span>
      <button type="button" className="text-button" aria-label={`移除关联 ${index + 1}`}
        onClick={() => change({ coverage: draft.coverage?.filter((_, i) => i !== index) })}>移除</button>
    </li>)}</ul>
    <Field label="添加关联需求"><select value={id} onChange={(e) => setID(e.target.value)}>
      <option value="">选择需求</option>{snapshot.requirements?.filter((r) => r.state !== 'withdrawn').map((r) =>
        <option key={r.id} value={r.id}>{r.title}</option>)}
    </select></Field>
    <Feedback error={error} />
    {id && plan && !current && <p>请先在“需求验收与补测”定义当前需求的验收条款。</p>}
    {current && plan.criteria?.map((criterion) => {
      const linked = draft.coverage?.some((ref) => ref.requirement_id === id && ref.criterion_id === criterion.id &&
        ref.revision === plan.revision && ref.plan_version === plan.version);
      return <label className="checkbox" key={criterion.id}><input type="checkbox" checked={linked ?? false} onChange={(e) => {
        const kept = draft.coverage?.filter((ref) => ref.requirement_id !== id || ref.criterion_id !== criterion.id) ?? [];
        change({ coverage: e.target.checked ? [...kept, { requirement_id: id, revision: plan.revision,
          plan_version: plan.version, criterion_id: criterion.id }] : kept });
      }} />{criterion.title}</label>;
    })}
  </fieldset>;
}
