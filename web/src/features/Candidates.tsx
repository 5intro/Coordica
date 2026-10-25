import type { Snapshot } from '../api.generated';
import { ApiClient } from '../api/client';
import { Empty } from '../components/ui';
import { PoolInspection } from '../components/material/PoolInspection';
import { useRouteField } from '../hooks/useRouteField';
import { CandidateDetail } from './candidates/CandidateDetail';
import { CandidateList } from './candidates/CandidateList';
import { CandidateToolbar } from './candidates/CandidateToolbar';
import { activeCandidate, candidateTitle, visibleCandidates } from './candidates/candidateView';

type Props = { snapshot: Snapshot; client: ApiClient; refresh: () => void; initialId?: string };

export function Candidates({ snapshot, client, refresh, initialId }: Props) {
  const candidates = snapshot.candidates ?? [];
  const defaultID = initialId ?? candidates.find((item) => activeCandidate(item.state))?.id ?? candidates[0]?.id ?? '';
  const [selected, setSelected] = useRouteField('candidates', 'id', defaultID);
  const [query, setQuery] = useRouteField('candidates', 'query');
  const [filter, setFilter] = useRouteField('candidates', 'filter', 'active');
  const [sort, setSort] = useRouteField('candidates', 'sort', 'newest');
  const title = (item: (typeof candidates)[number]) => candidateTitle(snapshot, item);
  const shown = visibleCandidates(snapshot, filter, query, sort);
  const candidate = candidates.find((item) => item.id === selected);
  return <>
    <CandidateToolbar candidates={candidates} query={query} filter={filter} sort={sort}
      onQuery={setQuery} onFilter={setFilter} onSort={setSort} />
    <div className="task-layout">
      <CandidateList items={shown} total={candidates.length} currentID={candidate?.id} title={title} select={setSelected} />
      <PoolInspection title={candidate ? title(candidate) : '审查详情'}>
        {candidate ? <>
          {!shown.some((item) => item.id === candidate.id) && <p className="muted" role="status">
            此详情不在当前筛选范围，仍保留所选候选。可切换“全部”或清除搜索查看列表。
          </p>}
          <CandidateDetail key={candidate.id} snapshot={snapshot} candidate={candidate} client={client} refresh={refresh} />
        </> : <div className="panel">
          <Empty title={selected ? '候选不存在' : '尚无候选证据'}>自测摘要不能代替评审与门禁事实。</Empty>
        </div>}
      </PoolInspection>
    </div>
  </>;
}
