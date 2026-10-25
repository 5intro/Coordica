import type { Candidate } from '../../api.generated';
import { SectionTitle, stateLabel } from '../../components/ui';

type Props = {
  candidates: Candidate[]; query: string; filter: string; sort: string;
  onQuery: (value: string) => void; onFilter: (value: string) => void; onSort: (value: string) => void;
};

export function CandidateToolbar({ candidates, query, filter, sort, onQuery, onFilter, onSort }: Props) {
  return <>
    <SectionTitle title="审查池"
      description="进行中包含候选准备、审查、验证、集成与结果核对；进入列表不代表审查通过。"
      filters={<div className="segmented" aria-label="列表范围">
        <button aria-pressed={filter === 'all'} onClick={() => onFilter('all')}>全部</button>
        <button aria-pressed={filter === 'active'} onClick={() => onFilter('active')}>进行中</button>
        <button aria-pressed={filter === 'history'} onClick={() => onFilter('history')}>历史</button>
      </div>} />
    <div className="task-toolbar">
      <input type="search" aria-label="搜索审查" placeholder="任务、需求 ID、候选版本或原因"
        value={query} onChange={(event) => onQuery(event.target.value)} />
      <details className="filter-options">
        <summary>筛选与排序</summary>
        <div className="actions">
          <select aria-label="审查筛选" value={filter} onChange={(event) => onFilter(event.target.value)}>
            <option value="active">进行中 · 准备至集成确认</option>
            <option value="all">全部候选</option>
            <option value="history">历史 · 失败 / 已合并 / 已失效</option>
            {[...new Set(candidates.map((item) => item.state))].map((state) =>
              <option key={state} value={state}>{stateLabel(state)}</option>)}
          </select>
          <select aria-label="审查排序" value={sort} onChange={(event) => onSort(event.target.value)}>
            <option value="newest">最新优先</option>
            <option value="oldest">最早优先</option>
            <option value="title">任务标题</option>
          </select>
        </div>
      </details>
    </div>
  </>;
}
