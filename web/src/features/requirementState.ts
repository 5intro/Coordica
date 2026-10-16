import type { Requirement, Task } from '../api.generated';
import { terminalTask } from './taskState';

export function activeRequirement(requirement: Requirement, tasks: Task[]) {
  if (requirement.state === 'withdrawn') return false;
  const current = tasks.filter((task) => task.requirement_ids?.includes(requirement.id)
    && (task.requirement_revisions?.[requirement.id] ?? 1) === (requirement.content_revision ?? 1));
  return !current.length || current.some((task) => !terminalTask(task.state));
}
