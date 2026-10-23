import { useEffect, useState } from 'react';
import type { TestCase, TestCasePage } from '../../api.generated';
import { messageOf } from '../../api/client';
import { categories, testCasePage } from '../../api/testCatalog';
import { Feedback, Field } from '../../components/ui';
import type { TestingProps } from '../Testing';
import { TestCaseEditor } from './TestCaseEditor';

export function TestCases({ snapshot, client, refresh, active = true }: TestingProps & { active?: boolean }) {
  const [category, setCategory] = useState('');
  const [after, setAfter] = useState('');
  const [page, setPage] = useState<TestCasePage>();
  const [selected, setSelected] = useState<TestCase>();
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true); setError(''); setPage(undefined);
    const params = new URLSearchParams({ limit: '50', after, category });
    testCasePage(client, params.toString(), controller.signal).then((v) => { if (!controller.signal.aborted) setPage(v); }).catch((e) => {
      if (!controller.signal.aborted) setError(messageOf(e));
    }).finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [client, category, after, revision]);
  const add = () => setSelected({ id: `case_${crypto.randomUUID().replaceAll('-', '')}`, version: 0, name: '', purpose: '',
    categories: category ? [category] : ['regression'], steps: [''], assertions: [''], enabled: true, implemented: false,
    suite_id: '', check_name: '', report_format: '', external_id: '', coverage: [] });
  return <section aria-label="测试条目" className="catalog-section">
    <div className="testing-heading">
      <Field label="测试分类"><select value={category} onChange={(e) => { setCategory(e.target.value); setAfter(''); }}>
        <option value="">全部分类</option>{Object.entries(categories).map(([id, label]) => <option key={id} value={id}>{label}</option>)}
      </select></Field>
      <div className="actions"><button className="secondary" onClick={() => setRevision((n) => n + 1)}>刷新条目</button>
        <button className="primary" onClick={add}>新增条目</button></div>
    </div>
    <Feedback error={error} />
    {loading && <p role="status">正在读取测试条目…</p>}
    <div className="catalog-layout">
      <div>
        {!loading && page && !page.items?.length && <p>此分类还没有测试条目。添加步骤与断言，明确要验证的行为。</p>}
        <ul className="catalog-list" aria-label="测试清单">
          {page?.items?.map((item) => <li key={item.id}>
            <button className="catalog-item" aria-pressed={selected?.id === item.id} onClick={() => setSelected(item)}>
              <strong>{item.name}</strong><span>{item.purpose}</span>
              <small>{item.categories?.map((id) => categories[id as keyof typeof categories] ?? id).join(' · ')}
                {' · '}{!item.enabled ? '已停用' : item.implemented ? '已实现' : '待实现'}</small>
            </button>
          </li>)}
        </ul>
        <div className="actions">
          {after && <button className="secondary" onClick={() => setAfter('')}>回到首批</button>}
          {page?.next && <button className="secondary" onClick={() => setAfter(page.next)}>下一批</button>}
        </div>
      </div>
      {selected ? <TestCaseEditor key={`${selected.id}:${selected.version}`} original={selected} snapshot={snapshot} client={client} active={active}
        onSaved={(value) => { setSelected(value); setRevision((n) => n + 1); refresh(); }} />
        : <div className="catalog-placeholder">选择一个条目查看步骤、断言与验收关联。</div>}
    </div>
  </section>;
}
