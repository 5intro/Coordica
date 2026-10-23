import type { Snapshot } from '../api.generated';
import { ApiClient } from '../api/client';
import { Feedback, SectionTitle } from '../components/ui';
import { useAction } from '../hooks/useAction';
import { BaselineView, InitializationConfirmation } from './initialization/BaselineView';
import { InitializationInterview } from './initialization/InitializationInterview';
import { InitializationOperations } from './initialization/InitializationOperations';
import { InitializationPolicy } from './initialization/InitializationPolicy';
import { PreparationEvidence } from './initialization/PreparationEvidence';
import './initialization/preparation.css';

type Props = { snapshot: Snapshot; client: ApiClient; refresh: () => void };

export function ProjectInitialization(props: Props) {
  return <ProjectPreparation key={props.snapshot.project?.id ?? 'no-project'} {...props} />;
}

function ProjectPreparation({ snapshot, client, refresh }: Props) {
  const action = useAction(refresh);
  const project = snapshot.project;
  const current = snapshot.initialization ?? null;
  return (
    <div className="preparation-page">
      <SectionTitle title="项目准备" />
      <Feedback {...action} />
      {!project ? <p>请先建立项目，再准备共享约定。</p> : (
        <>
          <InitializationOperations project={project} current={current} client={client} action={action} />
          {current && (
            <>
              <BaselineView current={current} />
              <InitializationInterview projectID={project.id} current={current} client={client} action={action} />
              <PreparationEvidence current={current} />
            </>
          )}
          <InitializationPolicy project={project} current={current} client={client} action={action} />
          {current && <InitializationConfirmation current={current} project={project} client={client} action={action} />}
        </>
      )}
    </div>
  );
}
