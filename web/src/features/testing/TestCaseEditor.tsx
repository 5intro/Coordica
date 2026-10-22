import { useState } from 'react';
import type { FormEvent } from 'react';
import type { Snapshot, TestCase } from '../../api.generated';
import { ApiClient } from '../../api/client';
import { categories, checked, isTestCase } from '../../api/testCatalog';
import { Feedback, Field } from '../../components/ui';
import { useAction } from '../../hooks/useAction';
import { CaseAcceptanceLinks } from './CaseAcceptanceLinks';

type Props = { original: TestCase; snapshot: Snapshot; client: ApiClient; onSaved: (value: TestCase) => void };
export function TestCaseEditor({ original, snapshot, client, onSaved }: Props) {
  const [draft, setDraft] = useState(original);
  const action = useAction(() => {});
  const change = (value: Partial<TestCase>) => setDraft((v) => ({ ...v, ...value }));
  async function save(event: FormEvent) {
    event.preventDefault();
    await action.execute(async () => {
      const value = checked(await client.write<unknown>('/tests/cases', { expected_version: original.version, case: draft }), isTestCase);
      if (value.id !== draft.id || value.version !== original.version + 1) throw new Error('保存回执与条目版本不一致，请刷新核对。');
      onSaved(value);
    }, '测试条目已保存。');
  }
  return <form className="panel catalog-detail testing-form" onSubmit={save}>
    <h2>{original.version ? '测试详情' : '新增测试条目'}</h2>
    <Field label="名称"><input required maxLength={200} value={draft.name} onChange={(e) => change({ name: e.target.value })} /></Field>
    <Field label="验证目的"><textarea required rows={2} value={draft.purpose} onChange={(e) => change({ purpose: e.target.value })} /></Field>
    <fieldset><legend>分类</legend><div className="actions">{Object.entries(categories).map(([id, label]) =>
      <label key={id}><input type="checkbox" checked={draft.categories?.includes(id) ?? false} onChange={(e) => change({
        categories: e.target.checked ? [...draft.categories ?? [], id] : draft.categories?.filter((v) => v !== id),
      })} />{label}</label>)}</div></fieldset>
    <Field label="操作步骤（每行一项）"><textarea required rows={3} value={draft.steps?.join('\n')}
      onChange={(e) => change({ steps: e.target.value.split('\n') })} /></Field>
    <Field label="预期断言（每行一项）"><textarea required rows={3} value={draft.assertions?.join('\n')}
      onChange={(e) => change({ assertions: e.target.value.split('\n') })} /></Field>
    <div className="actions"><label><input type="checkbox" checked={draft.enabled}
      onChange={(e) => change({ enabled: e.target.checked })} />启用</label>
      <label><input type="checkbox" checked={draft.implemented}
        onChange={(e) => change({ implemented: e.target.checked })} />已实现</label></div>
    {draft.implemented && <CaseExecution draft={draft} snapshot={snapshot} change={change} />}
    <CaseAcceptanceLinks draft={draft} snapshot={snapshot} client={client} change={change} />
    <Feedback error={action.error} notice={action.notice} />
    <button className="primary" disabled={action.busy || !draft.categories?.length}>{action.busy ? '正在保存…' : '保存条目'}</button>
  </form>;
}

function CaseExecution({ draft, snapshot, change }: { draft: TestCase; snapshot: Snapshot; change: (v: Partial<TestCase>) => void }) {
  const suite = snapshot.test_suites?.find((v) => v.id === draft.suite_id);
  return <fieldset><legend>执行与报告</legend>
    <Field label="测试套件"><select required value={draft.suite_id} onChange={(e) => change({ suite_id: e.target.value, check_name: '' })}>
      <option value="">选择套件</option>{snapshot.test_suites?.map((v) => <option key={v.id} value={v.id}>{v.name}</option>)}
    </select></Field>
    <Field label="检查命令"><select required value={draft.check_name} onChange={(e) => change({ check_name: e.target.value })}>
      <option value="">选择检查</option>{suite?.checks?.map((v) => <option key={v.name}>{v.name}</option>)}
    </select></Field>
    <Field label="条目报告"><select value={draft.report_format} onChange={(e) => change({ report_format: e.target.value })}>
      <option value="">仅有命令结果</option><option value="go-json">Go test JSON</option>
    </select></Field>
    {draft.report_format === 'go-json' ? <Field label="报告测试标识"><input required value={draft.external_id} placeholder="package#TestName"
      onChange={(e) => change({ external_id: e.target.value.trim() })} /></Field> : <small>命令结果不代表这个条目已通过。</small>}
  </fieldset>;
}
