import { useState } from 'react';
import type { Snapshot } from '../api.generated';
import type { Page } from '../App';
import { DetailPanel } from '../components/material/DetailPanel';
import { Glass } from '../components/material/Glass';
import { blockRecords, matches } from './management';
import { reasonLabel } from '../components/ui';

export function WorkspaceSearch({ snapshot, onClose, navigate }: { snapshot: Snapshot; onClose: () => void; navigate: (page: Page, id: string) => void }) {
  const [query,setQuery] = useState('');
  const entries: { page: Page; id: string; title: string; category: string; content?: string }[] = [
    ...(snapshot.requirements ?? []).map((r) => ({ page:'requirements' as Page, id:r.id, title:r.title, category:'需求', content:r.body })),
    ...(snapshot.tasks ?? []).map((t) => ({ page:'tasks' as Page, id:t.id, title:t.title, category:'任务', content:t.description })),
    ...(snapshot.candidates ?? []).map((c) => ({ page:'candidates' as Page, id:c.id, title:snapshot.tasks?.find((t) => t.id === c.task_id)?.title || '未命名任务', category:'审查', content:c.commit })),
    ...blockRecords(snapshot).map((b) => ({ page:'blocks' as Page, id:b.decision?.id || b.id, title:b.task?.title || reasonLabel(b.reason), category:'阻断', content:b.reason })),
  ];
  const shown = entries.filter((e) => matches(query,[e.title,e.content,e.id]));
  return <DetailPanel title="搜索工作台" onClose={onClose}><h2>搜索工作台</h2><input autoFocus type="search" aria-label="搜索工作" placeholder="搜索需求、任务、审查与阻断" value={query} onChange={(e) => setQuery(e.target.value)}/><p className="inspection-meta">{shown.length} 项结果</p><div className="inspection-related">{shown.slice(0,30).map((e) => <Glass radius={14} className="work-card" key={`${e.page}:${e.id}`}><button onClick={() => navigate(e.page,e.id)}><strong>{e.title}</strong><small>{e.category}</small></button></Glass>)}{!shown.length && <p className="muted">没有找到相关工作。</p>}{shown.length > 30 && <p className="muted">先显示 30 项，请输入关键词缩小范围。</p>}</div></DetailPanel>;
}
