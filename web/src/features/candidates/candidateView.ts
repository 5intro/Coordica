import type { Candidate, Snapshot } from '../../api.generated';
import { matches } from '../management';

// Display groups follow the transitions in internal/app/integration.go.
export const activeCandidate = (state: string) => [
  'accepted', 'prechecking', 'reviewing', 'validating', 'ready_to_land', 'landing', 'reconciling',
].includes(state);
const historicalCandidate = (state: string) => ['failed', 'landed', 'rejected', 'invalidated', 'cancelled'].includes(state);
export const candidateStageGuidance: Record<string, string> = {
  accepted: '已受理 · 等待构建固定候选版本；尚未进入独立审查。',
  prechecking: '预检查中 · 尚未进入 Agent 独立审查。',
  reviewing: '独立审查待审 / 审中，等待固定版本的评审结论。',
  validating: '审查后验证中 · 等待固定候选的程序门禁结果，尚未进入主线。',
  ready_to_land: '审查通过 · 待集成。有效快速检查证据由中心保留；无需再次执行全量回归。最终放行仍由中心核验当前授权与主线。',
  landing: '正在集成 · 等待主线写入结果确认，尚不能视为已合并。',
  reconciling: '正在核对集成结果 · 以远端主线及适用的 PR 凭据为准；结果未确认前，不应重复提交。若需人工处理，请查看下方原因与开发任务。',
};

export const candidateTitle = (snapshot: Snapshot, item: Candidate) =>
  snapshot.tasks?.find((task) => task.id === item.task_id)?.title ?? '未命名任务';

export function visibleCandidates(snapshot: Snapshot, filter: string, query: string, sort: string): Candidate[] {
  return (snapshot.candidates ?? []).filter((item) => {
    const stateMatches = filter === 'all' || (filter === 'active' ? activeCandidate(item.state)
      : filter === 'history' ? historicalCandidate(item.state) : item.state === filter);
    const requirements = snapshot.tasks?.find((task) => task.id === item.task_id)?.requirement_ids ?? [];
    return stateMatches && matches(query, [
      item.id, candidateTitle(snapshot, item), item.commit, item.state, item.detail, ...requirements,
    ]);
  }).sort((a, b) => sort === 'title'
    ? candidateTitle(snapshot, a).localeCompare(candidateTitle(snapshot, b), 'zh-CN')
    : (sort === 'oldest' ? 1 : -1) * a.created_at.localeCompare(b.created_at));
}
