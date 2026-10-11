import { emptyPhaseB } from './phaseB';
import type { ConnectionInfo, Project, Snapshot } from '../api.generated';

export const connectionInfo: ConnectionInfo = {
  api_version: 'v1', version: '0.1.3-dev', runner_listen: '192.0.2.1:8443', console_listen: '127.0.0.1:8080',
  console_url: 'http://127.0.0.1:8080', runner_url: 'https://192.0.2.1:8443', runner_console_url: 'http://127.0.0.1:8081',
  data_dir: '/private/center-data-do-not-share', certificate_pem: '-----BEGIN CERTIFICATE-----\nPUBLIC-CA\n-----END CERTIFICATE-----\n',
  certificate_sha256: '0123456789abcdef',
};

export const centerProject: Project = {
  id: 'project', version: 1, name: '协作项目', repository_url: 'https://github.com/example/project.git', target_branch: 'main',
  paused: false, total_budget_seconds: 3600, used_seconds: 60, reserved_seconds: 30,
  protected_paths: ['tools/**'], checks: [{ name: 'regression', command: '/usr/bin/node', args: ['--test'], timeout_seconds: 30 }],
  policy_version: 1, created_at: '2026-10-11T05:00:00Z',
};

export const centerSnapshot: Snapshot = { ...emptyPhaseB, api_version: 'v1', project: centerProject, requirements: [], tasks: [], runs: [], candidates: [], nodes: [], events: [] };
