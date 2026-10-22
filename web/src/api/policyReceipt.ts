import type { Project, ProjectPolicyRequest } from '../api.generated';

const object = (value: unknown): value is Record<string, unknown> =>
  !!value && typeof value === 'object' && !Array.isArray(value);
const strings = (value: unknown): value is string[] =>
  Array.isArray(value) && value.every((part) => typeof part === 'string');

export function validPolicyReceipt(value: unknown, projectID: string, request: ProjectPolicyRequest): value is Project {
  if (!object(value) || value.id !== projectID || value.version !== request.expected_version + 1
    || value.paused !== true || !Number.isSafeInteger(value.policy_version) || Number(value.policy_version) < 1
    || !['name', 'repository_url', 'target_branch', 'created_at'].every((key) => typeof value[key] === 'string')
    || !['used_seconds', 'reserved_seconds'].every((key) => Number.isSafeInteger(value[key]) && Number(value[key]) >= 0)
    || !strings(value.protected_paths) || !Array.isArray(value.checks) || value.checks.length === 0) return false;
  return value.checks.every((check: unknown) => object(check) && typeof check.name === 'string'
    && typeof check.command === 'string' && strings(check.args) && Number.isSafeInteger(check.timeout_seconds)
    && Number(check.timeout_seconds) >= 1 && Number(check.timeout_seconds) <= 3600);
}
