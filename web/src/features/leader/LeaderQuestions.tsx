import type { Snapshot } from '../../api.generated';
import { ApiClient } from '../../api/client';
import { reasonLabel } from '../../components/ui';
import { useAction } from '../../hooks/useAction';
import { InitializationInterview } from '../initialization/InitializationInterview';
import { PlanningQuestions } from './PlanningQuestions';
import { blockRecords } from '../management';

export function LeaderQuestions({ snapshot, client, refresh }: { snapshot: Snapshot; client: ApiClient; refresh: () => void }) {
  const action = useAction(refresh);
  const blocks = blockRecords(snapshot);
  const initialization = snapshot.initialization;
  const needsInterview = initialization && ['interview', 'awaiting_owner'].includes(initialization.status);
  const pendingAnswer = client.initializationAnswers.read(snapshot.project?.id ?? '')?.pending;
  return (
    <div className="leader-inbox">
      {snapshot.project && initialization && (needsInterview || pendingAnswer) && (
        <InitializationInterview projectID={snapshot.project.id} current={initialization} client={client} action={action} />
      )}
      <PlanningQuestions snapshot={snapshot} client={client} refresh={refresh} />
      <section className="panel" aria-labelledby="leader-decisions-title">
        <header className="leader-section-header"><h2 id="leader-decisions-title">待决策与阻断</h2><span>{blocks.length}</span></header>
        {blocks.length ? blocks.slice(0, 20).map((block) => (
          <article className="leader-question-card" key={block.id}>
            <strong>{reasonLabel(block.reason) === block.reason ? '需要确认' : reasonLabel(block.reason)}</strong>
            <p>{block.task?.title || ({ planning: '目标规划', initialization: '项目准备', scope_request: '范围申请' }[block.source] ?? '开发事项')}</p>
            {block.evidence && <p className="preserve-lines">{block.evidence}</p>}
            <a className="text-button" href={`#blocks?id=${encodeURIComponent(block.id)}`}>查看并处理</a>
          </article>
        )) : <p className="muted">暂无待处理的决策或开发阻断。</p>}
        {blocks.length > 20 && <a className="text-button" href="#blocks">查看其余事项</a>}
      </section>
    </div>
  );
}
