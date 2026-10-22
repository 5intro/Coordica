import type { Snapshot } from '../../api.generated';

export function LeaderEntry({ snapshot }: { snapshot: Snapshot }) {
  const initialization = snapshot.initialization;
  const planning = snapshot.planning_document?.leader_result;
  const questions = (['interview', 'awaiting_owner'].includes(initialization?.status ?? '')
    ? initialization?.result?.questions?.length ?? 0 : 0)
    + new Set([...(planning?.clarifications ?? []), ...(planning?.missing_items ?? [])]).size;
  const active = snapshot.planning_runs?.some((run) => ['running', 'cancel_requested', 'unknown_live'].includes(run.state))
    || ['running', 'cancel_requested', 'unknown_live'].includes(initialization?.operation_state ?? '');
  return (
    <a className="overview-leader-entry" href="#planning">
      <strong>Leader</strong>
      <span>{active ? '有活动记录' : snapshot.project?.paused ? '项目已暂停' : '查看工作进展'}</span>
      <span>{questions ? `${questions} 项待回答` : '打开对话追踪'} →</span>
    </a>
  );
}
