import type { Snapshot } from '../api.generated';
import { emptyPhaseB } from './phaseB';
import { readyInitialization } from './initialization';

export const preparationSnapshot: Snapshot = {
  ...emptyPhaseB,
  api_version: 'v1',
  initialization: readyInitialization,
  project: {
    id: 'project', version: 1, name: 'Project', repository_url: '/fixed/source', target_branch: 'main',
    paused: true, used_seconds: 0, reserved_seconds: 0, protected_paths: ['internal/app/**'],
    checks: readyInitialization.result!.baseline.checks, policy_version: 1, created_at: '',
  },
  requirements: [], tasks: [], runs: [], candidates: [], nodes: [], events: [],
};
