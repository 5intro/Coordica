import type { AcceptanceCoverage, AcceptanceSignoff, AcceptanceSignoffRequest, AcceptanceSignoffStatus } from '../api.generated';
import { ApiError } from './client';
import type { ApiClient } from './client';

export type PendingSignoff = {
  key: string; body: AcceptanceSignoffRequest; revision: number; planVersion: number;
  clauses: { id: string; title: string }[];
};

// Unknown outcomes retain their original declaration in this credential's
// memory, even across route/coverage changes. They never become a new action.
export class AcceptanceSignoffActions {
  private readonly requests = new Map<string, PendingSignoff>();
  pending(id: string, commit: string) { return this.requests.get(`${id}\n${commit}`); }
  begin(coverage: AcceptanceCoverage, body: AcceptanceSignoffRequest): PendingSignoff {
    const previous = this.pending(body.requirement_id, body.commit);
    if (previous) return previous;
    const pending = { key: crypto.randomUUID(), body: structuredClone(body), revision: coverage.plan.revision,
      planVersion: coverage.plan.version, clauses: (coverage.criteria ?? []).filter((v) => v.criterion.method === 'manual')
        .map((v) => ({ id: v.criterion.id, title: v.criterion.title })) };
    this.requests.set(`${body.requirement_id}\n${body.commit}`, pending);
    return pending;
  }
  settle(pending: PendingSignoff) {
    if (this.pending(pending.body.requirement_id, pending.body.commit)?.key === pending.key) {
      this.requests.delete(`${pending.body.requirement_id}\n${pending.body.commit}`);
    }
  }
}

const object = (v: unknown): v is Record<string, unknown> => !!v && typeof v === 'object' && !Array.isArray(v);
const positive = (v: unknown) => Number.isSafeInteger(v) && Number(v) > 0;
const digest = (v: unknown): v is string => typeof v === 'string' && /^[a-f0-9]{64}$/.test(v);

export function isSignoff(v: unknown): v is AcceptanceSignoff {
  return object(v) && ['id', 'requirement_id', 'commit', 'note', 'actor_id', 'recorded_at'].every((k) =>
    typeof v[k] === 'string' && String(v[k]).trim().length > 0) && positive(v.version) && positive(v.revision)
    && positive(v.plan_version) && digest(v.coverage_digest) && ['accepted', 'revoked'].includes(String(v.state))
    && (v.manual_criteria === null || Array.isArray(v.manual_criteria) && v.manual_criteria.length <= 32
      && v.manual_criteria.every((id) => typeof id === 'string')) && Number.isFinite(Date.parse(String(v.recorded_at)));
}

export function signoffStatus(value: unknown, coverage: AcceptanceCoverage): AcceptanceSignoffStatus {
  const ready = coverage.current && !!coverage.criteria?.length && coverage.criteria.every((item) =>
    item.criterion.method === 'manual' || item.criterion.method === 'automated' && item.state === 'passed');
  const manualIDs = (coverage.criteria ?? []).filter((v) => v.criterion.method === 'manual').map((v) => v.criterion.id).sort();
  if (!object(value) || value.requirement_id !== coverage.requirement.id || value.commit !== coverage.commit
    || value.digest !== coverage.digest || typeof value.can_accept !== 'boolean' || typeof value.accepted !== 'boolean'
    || !(value.receipt === null || isSignoff(value.receipt)) || !Array.isArray(value.recent) || value.recent.length > 10
    || !value.recent.every((v) => isSignoff(v) && v.requirement_id === value.requirement_id && v.commit === value.commit)
    || value.receipt !== null && (value.receipt.requirement_id !== value.requirement_id || value.receipt.commit !== value.commit)
    || value.can_accept && !ready
    || value.accepted && (!value.can_accept || !coverage.current || !value.receipt || value.receipt.state !== 'accepted'
      || value.receipt.coverage_digest !== coverage.digest || value.receipt.revision !== coverage.plan.revision
      || value.receipt.plan_version !== coverage.plan.version
      || JSON.stringify([...(value.receipt.manual_criteria ?? [])].sort()) !== JSON.stringify(manualIDs))) {
    throw new ApiError('invalid_response', '验收记录与当前覆盖不一致，请重新核对。');
  }
  return value as unknown as AcceptanceSignoffStatus;
}

export async function submitSignoff(client: ApiClient, coverage: AcceptanceCoverage, body: AcceptanceSignoffRequest) {
  const pending = client.acceptanceSignoffs.begin(coverage, body);
  try {
    const value = await client.write<unknown>('/tests/acceptance/signoff', pending.body, pending.key);
    const inBody = pending.body;
    if (!isSignoff(value) || value.requirement_id !== inBody.requirement_id || value.commit !== inBody.commit
      || value.version !== inBody.expected_version + 1 || value.note !== inBody.note
      || value.state !== (inBody.action === 'accept' ? 'accepted' : 'revoked')
      || inBody.action === 'accept' && (value.coverage_digest !== inBody.expected_digest
        || JSON.stringify([...(value.manual_criteria ?? [])].sort()) !== JSON.stringify([...(inBody.manual_criteria ?? [])].sort()))) {
      throw new ApiError('invalid_response', '验收回执不一致，原提交已保留，请重试确认结果。');
    }
    client.acceptanceSignoffs.settle(pending);
    return value;
  } catch (error) {
    if (error instanceof ApiError && error.status >= 400 && error.status < 500 && error.code !== 'invalid_response') {
      client.acceptanceSignoffs.settle(pending);
    }
    throw error;
  }
}
