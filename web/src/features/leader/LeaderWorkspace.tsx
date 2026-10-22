import type { Snapshot } from '../../api.generated';
import { ApiClient } from '../../api/client';
import { SectionTitle } from '../../components/ui';
import { Planning as PlanningControls } from '../Planning';
import { LeaderActivityPanel } from './LeaderActivityPanel';
import { LeaderQuestions } from './LeaderQuestions';
import './leader.css';

type Props = { snapshot: Snapshot; client: ApiClient; refresh: () => void; initialId?: string };

export function Planning(props: Props) {
  return (
    <div className="leader-workspace">
      <SectionTitle title="Leader 对话追踪" />
      <div className="leader-tracking-layout">
        <LeaderActivityPanel key={props.snapshot.project?.id ?? 'empty'} client={props.client} />
        <LeaderQuestions snapshot={props.snapshot} client={props.client} refresh={props.refresh} />
      </div>
      <details className="panel leader-controls" open={!!props.initialId}>
        <summary>目标、咨询与规划操作</summary>
        <PlanningControls {...props} />
      </details>
    </div>
  );
}
