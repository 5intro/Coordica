import type { NodeSnapshot, Requirement, RequirementProposal } from '../api.generated';
import { ApiClient, ApiError } from './client';

// The Runner's loopback proxy owns its node credential. Browser requests stay
// on that machine's same-origin page and never obtain an owner or node token.
export class LocalClient {
  private readonly transport = new ApiClient('');

  async info(signal?: AbortSignal): Promise<'center' | 'runner'> {
    const info = await this.transport.read<{ mode: string }>('/local/info', signal);
    if (!info || (info.mode !== 'center' && info.mode !== 'runner')) {
      throw new ApiError('incompatible_response', '本机页面类型无法识别，请更新 Coordica。');
    }
    return info.mode;
  }

  async snapshot(signal?: AbortSignal): Promise<NodeSnapshot> {
    const snapshot = await this.transport.read<NodeSnapshot>('/local/state', signal);
    if (!snapshot || snapshot.api_version !== 'v1' || typeof snapshot.node?.id !== 'string'
      || typeof snapshot.node.name !== 'string' || typeof snapshot.node.platform !== 'string'
      || typeof snapshot.project_name !== 'string' || typeof snapshot.project_paused !== 'boolean'
      || !(snapshot.node.capabilities === null || Array.isArray(snapshot.node.capabilities))
      || (snapshot.node.capabilities ?? []).some((capability) => !capability || typeof capability.tool !== 'string')
      || !(['tasks', 'runs', 'requirements'] as const).every((key) => Array.isArray(snapshot[key]))) {
      throw new ApiError('incompatible_response', '本机状态契约不兼容，请更新 Coordica。');
    }
    const ownRuns = snapshot.runs ?? [];
    if (ownRuns.some((run) => !run || typeof run.started_at !== 'string')) {
      throw new ApiError('incompatible_response', '本机执行记录格式无效，请更新 Coordica。');
    }
    const taskIDs = new Set(ownRuns.map((run) => run.task_id));
    if (ownRuns.some((run) => !run || run.node_id !== snapshot.node.id || typeof run.started_at !== 'string')
      || (snapshot.requirements ?? []).some((requirement) => !requirement || requirement.source_node_id !== snapshot.node.id || typeof requirement.created_at !== 'string')
      || (snapshot.tasks ?? []).some((task) => !task || !taskIDs.has(task.id))) {
      throw new ApiError('incompatible_response', '本机状态包含不属于此节点的记录，已停止显示。');
    }
    return snapshot;
  }

  submitRequirement(proposal: RequirementProposal): Promise<Requirement> {
    return this.transport.write('/local/requirements', proposal, undefined, { 'X-Coordica-Local': '1' });
  }
}
