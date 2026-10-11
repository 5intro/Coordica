import type { NodeSnapshot, Run, Task } from '../api.generated';

export const localTask: Task = { goal_version: 0, goal_authorization_version: 0,
  id: 'task_local', version: 1, title: '规范化问候姓名', description: '处理空白', acceptance: '检查通过',
  requirement_ids: [], dependencies: [], allowed_paths: ['src/**'], hot_resources: [], tool: 'dsh',
  state: 'executing', hold_reason: '', approved_version: 1, authorization_version: 1, generation: 1,
  budget_seconds: 300, used_seconds: 0, reserved_seconds: 60, max_run_seconds: 60,
  max_repairs: 1, max_retries: 1, repairs: 0, retries: 0, current_run_id: 'run_local',
  current_candidate_id: '', created_at: '2026-10-11T05:00:00Z',
};

export const localRun: Run = { goal_version: 0, goal_authorization_version: 0,
  id: 'run_local', version: 1, task_id: localTask.id, node_id: 'node_local', kind: 'implement', tool: 'dsh',
  state: 'running', generation: 1, authorization_version: 1, policy_version: 1,
  lease_until: '2026-10-11T05:30:00Z', deadline: '2026-10-11T05:40:00Z',
  grant_seconds: 60, elapsed_seconds: 12, base: 'fixed-base', head: '', candidate_id: '',
  session_id: '', detail: '正在运行验收检查', quiescent: false, started_at: '2026-10-11T05:00:00Z',
};

export const nodeSnapshot: NodeSnapshot = {
  api_version: 'v1', project_name: '双机验收项目', project_paused: false,
  node: { draining: false, disabled_new_work: false, scheduling_configured: false, tools: [], id: 'node_local', version: 1, name: 'Windows-dsh', platform: 'windows/amd64', capacity: 1,
    revoked: false, last_seen: '2026-10-11T05:00:00Z', capabilities: [{ tool: 'dsh', version: '0.2.0-rc.2',
      available: true, structured: true, protocol: 'acp', review: true, read_only: false,
      resume: false, cancel: true, detail: '' }] },
  tasks: [localTask], runs: [localRun], requirements: [],
};
