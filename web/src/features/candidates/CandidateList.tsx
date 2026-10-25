import type { Candidate } from '../../api.generated';
import { Glass } from '../../components/material/Glass';
import { Empty, Status, short } from '../../components/ui';

type Props = {
  items: Candidate[]; total: number; currentID?: string;
  title: (item: Candidate) => string; select: (id: string) => void;
};

export function CandidateList({ items, total, currentID, title, select }: Props) {
  return <section className="panel task-list collection-list" aria-label="候选列表">
    {items.length ? items.map((item) => <Glass className="work-card" radius={14} key={item.id}>
      <button className={`task-row ${item.id === currentID ? 'selected' : ''}`} key={item.id}
        aria-pressed={item.id === currentID} onClick={() => select(item.id)}>
        <div className="record-meta"><Status value={item.state} /></div>
        <strong>{title(item)}</strong>
        <small>{item.commit ? `C ${short(item.commit)}` : '固定候选尚未生成'}
          {item.protected ? ' · 受保护变化' : ''}
          {item.pull_request?.number ? ` · PR #${item.pull_request.number}` : ''}
        </small>
      </button>
    </Glass>) : <Empty title={total ? '没有匹配候选' : '集成队列为空'}>
      可切换历史，查看失败与已合并的固定版本。
    </Empty>}
  </section>;
}
