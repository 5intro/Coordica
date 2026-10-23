import type { AcceptanceCoverage, AcceptanceSignoff, AcceptanceSignoffStatus } from '../api.generated';

export const signoffCoverage: AcceptanceCoverage = {
  requirement: { id: 'req_pay', title: '一次支付', body: '重试仅支付一次', version: 2, content_revision: 1, state: 'active', created_at: '' },
  plan: { requirement_id: 'req_pay', revision: 1, version: 2, criteria: [
    { id: 'once', title: '重试不重复支付', method: 'automated' }, { id: 'experience', title: '支付结果清晰', method: 'manual' },
  ] },
  current: true, verified: false, commit: 'a'.repeat(40), digest: 'b'.repeat(64), criteria: [
    { criterion: { id: 'once', title: '重试不重复支付', method: 'automated' }, state: 'passed', cases: [
      { case: { id: 'pay_once', name: '支付重试', version: 1 }, state: 'passed', run_id: 'verification', platform: 'darwin' },
    ] },
    { criterion: { id: 'experience', title: '支付结果清晰', method: 'manual' }, state: 'manual_required', cases: [] },
  ], gaps: [{ criterion_id: 'experience', case_id: '', reason: 'manual_required', action: 'owner_acceptance' }],
};

export const signoffReceipt: AcceptanceSignoff = {
  id: 'signed_pay', requirement_id: 'req_pay', revision: 1, plan_version: 2, version: 1, commit: signoffCoverage.commit,
  coverage_digest: signoffCoverage.digest, manual_criteria: ['experience'], state: 'accepted', note: '重试后反馈清晰',
  actor_id: 'owner', recorded_at: '2026-10-23T04:00:00Z',
};

export const unsignedStatus: AcceptanceSignoffStatus = {
  requirement_id: 'req_pay', commit: signoffCoverage.commit, digest: signoffCoverage.digest,
  can_accept: true, accepted: false, receipt: null, recent: [],
};

export const signedStatus: AcceptanceSignoffStatus = { ...unsignedStatus, accepted: true,
  receipt: signoffReceipt, recent: [signoffReceipt] };
