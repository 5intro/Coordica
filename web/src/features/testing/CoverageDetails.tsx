import { useState } from 'react';
import type { AcceptanceCoverage } from '../../api.generated';
import { ApiClient } from '../../api/client';
import { checked, evidenceLabel, gapActionLabel, isSupplement } from '../../api/testCatalog';
import { Feedback } from '../../components/ui';
import { useAction } from '../../hooks/useAction';

type Props = { coverage: AcceptanceCoverage; client: ApiClient; refresh: () => void };
export function CoverageDetails({ coverage, client, refresh }: Props) {
  const action = useAction(refresh);
  const [requirementID, setRequirementID] = useState('');
  async function supplement() {
    await action.execute(async () => {
      const plan = checked(await client.write<unknown>('/tests/supplements', { requirement_id: coverage.requirement.id,
        commit: coverage.commit, expected_digest: coverage.digest }), isSupplement);
      if (plan.source_id !== coverage.requirement.id || plan.digest !== coverage.digest || plan.commit !== coverage.commit) {
        throw new Error('补测回执与预览不一致，请刷新核对。');
      }
      setRequirementID(plan.requirement_id);
    }, '补测计划已进入需求池。');
  }
  const caseName = (id: string) => coverage.criteria?.flatMap((v) => v.cases ?? []).find((v) => v.case.id === id)?.case.name;
  const clauseTitle = (id: string) => coverage.criteria?.find((v) => v.criterion.id === id)?.criterion.title ?? '验收计划';
  return <div className="coverage-detail">
    <p role="status">{coverage.verified ? '当前提交的自动测试证据齐备' : '验收证据待补充'}
      <small>需求修订 {coverage.plan.revision} · 验收 v{coverage.plan.version} · {coverage.commit.slice(0, 12)}</small></p>
    <ul className="catalog-list">{coverage.criteria?.map((item) => <li key={item.criterion.id} className="panel criterion-row">
      <div className="testing-heading"><h3>{item.criterion.title}</h3><span>{evidenceLabel(item.state)}</span></div>
      <ul>{item.cases?.map((e) => <li key={e.case.id}>
        <span>{e.case.name} · {evidenceLabel(e.state)}</span>
        {e.run_id && <a href={`#testing?run=${encodeURIComponent(e.run_id)}`}>查看执行</a>}
      </li>)}</ul>
    </li>)}</ul>
    {!!coverage.gaps?.length && <section className="panel supplement-preview" aria-label="补测计划预览">
      <h2>补测计划</h2>
      <ul>{coverage.gaps.map((gap, i) => <li key={i}><strong>{clauseTitle(gap.criterion_id)}</strong>
        <span>{caseName(gap.case_id)}{gap.case_id && ' · '}{evidenceLabel(gap.reason)} · {gapActionLabel(gap.action)}</span></li>)}</ul>
      {requirementID ? <a className="secondary" href={`#requirements?id=${encodeURIComponent(requirementID)}`}>查看补测需求</a>
        : <button className="primary" onClick={() => void supplement()}
          disabled={action.busy || !coverage.current || !coverage.gaps?.some((g) => g.action !== 'owner_acceptance')}>
          {action.busy ? '正在生成…' : '生成补测需求'}</button>}
      <small>Leader 沿现有流程处理；项目暂停时不执行。</small>
    </section>}
    <Feedback error={action.error} notice={action.notice} />
  </div>;
}
