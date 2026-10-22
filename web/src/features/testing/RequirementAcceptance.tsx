import { useEffect, useState } from 'react';
import type { AcceptanceCoverage, AcceptancePlan } from '../../api.generated';
import { messageOf } from '../../api/client';
import { checked, isAcceptancePlan, isCoverage } from '../../api/testCatalog';
import { Feedback, Field } from '../../components/ui';
import type { TestingProps } from '../Testing';
import { AcceptanceEditor } from './AcceptanceEditor';
import { CoverageDetails } from './CoverageDetails';

export function RequirementAcceptance({ snapshot, client, refresh }: TestingProps) {
  const [id, setID] = useState('');
  const [plan, setPlan] = useState<AcceptancePlan>();
  const [editing, setEditing] = useState(false);
  const [commit, setCommit] = useState('');
  const [coverage, setCoverage] = useState<AcceptanceCoverage>();
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const [revision, setRevision] = useState(0);
  const requirement = snapshot.requirements?.find((v) => v.id === id);
  const requirementVersion = requirement?.version;
  useEffect(() => {
    setPlan(undefined); setCoverage(undefined); setError(''); setEditing(false);
    if (!id) return;
    const controller = new AbortController();
    client.read<unknown>(`/tests/acceptance/plan?requirement_id=${encodeURIComponent(id)}`, controller.signal)
      .then((v) => { if (!controller.signal.aborted) setPlan(checked(v, isAcceptancePlan)); })
      .catch((e) => { if (!controller.signal.aborted) setError(messageOf(e)); });
    return () => controller.abort();
  }, [client, id, revision, requirementVersion]);
  useEffect(() => { setCoverage(undefined); setError(''); }, [commit]);
  const validCommit = /^(?:[a-f0-9]{40}|[a-f0-9]{64})$/.test(commit) && !/^0+$/.test(commit);
  async function query() {
    setLoading(true); setCoverage(undefined); setError('');
    try {
      const v = checked(await client.read<unknown>(`/tests/acceptance?requirement_id=${encodeURIComponent(id)}&commit=${commit}`), isCoverage);
      if (v.requirement.id !== id || v.commit !== commit) throw new Error('覆盖报告与查询条件不一致，请重试。');
      setCoverage(v);
    } catch (e) { setError(messageOf(e)); } finally { setLoading(false); }
  }
  const currentCoverage = coverage && coverage.requirement.version === requirementVersion && coverage.commit === commit ? coverage : undefined;
  return <section className="catalog-section" aria-label="需求验收与补测">
    <div className="testing-query"><Field label="关联需求"><select value={id} disabled={loading}
      onChange={(e) => setID(e.target.value)}><option value="">选择需求</option>
      {snapshot.requirements?.map((r) => <option key={r.id} value={r.id}>{r.title}{r.state === 'withdrawn' ? '（已撤销）' : ''}</option>)}
    </select></Field>
      <button className="secondary" disabled={!plan || loading || requirement?.state === 'withdrawn'}
        onClick={() => setEditing((v) => !v)}>{editing ? '收起编辑' : plan?.version ? '编辑验收条款' : '定义验收条款'}</button>
    </div>
    <Feedback error={error} />
    {editing && plan && requirement && <AcceptanceEditor key={`${id}:${plan.version}:${requirement.version}`}
      plan={plan} requirement={requirement} client={client} onSaved={() => { setRevision((n) => n + 1); refresh(); }} />}
    {requirement && <>
      <div className="testing-query">
        <Field label="验收固定提交"><input value={commit} disabled={loading} placeholder="完整提交 SHA"
          onChange={(e) => setCommit(e.target.value.trim().toLowerCase())} /></Field>
        <button className="primary" disabled={!validCommit || loading || !plan} onClick={() => void query()}>
          {loading ? '正在核对…' : '检查覆盖并预览补测'}</button>
      </div>
      {!currentCoverage && !loading && <p>按固定提交核对条目证据。测试验证与人工验收分别保留。</p>}
      {currentCoverage && <CoverageDetails key={currentCoverage.digest} coverage={currentCoverage} client={client} refresh={refresh} />}
    </>}
  </section>;
}
