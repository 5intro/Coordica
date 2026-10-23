import { expect, it, vi } from 'vitest';
import { ApiClient, ApiError } from './client';
import { signoffStatus, submitSignoff } from './acceptanceSignoffs';
import type { AcceptanceSignoffRequest } from '../api.generated';
import { signoffCoverage as coverage, signoffReceipt as receipt, signedStatus } from '../test/acceptanceSignoff';

const request: AcceptanceSignoffRequest = { requirement_id: 'req_pay', commit: coverage.commit, action: 'accept',
  expected_version: 0, expected_digest: coverage.digest, manual_criteria: ['experience'], note: receipt.note };

it('keeps malformed and mismatched successful receipts pending with an identical retry key', async () => {
  for (const broken of [{ ...receipt, version: 2 }, { ...receipt, coverage_digest: 'f'.repeat(64) },
    { ...receipt, manual_criteria: [] }, { ...receipt, note: 'different declaration' }, { version: 1 }]) {
    const client = new ApiClient('owner');
    const write = vi.spyOn(client, 'write').mockResolvedValueOnce(broken).mockResolvedValue(receipt);
    await expect(submitSignoff(client, coverage, request)).rejects.toMatchObject({ code: 'invalid_response' });
    expect(client.acceptanceSignoffs.pending(request.requirement_id, request.commit)?.body).toEqual(request);
    await submitSignoff(client, coverage, { ...request, note: 'new input must not replace an unknown result' });
    expect(write.mock.calls[1]).toEqual(write.mock.calls[0]);
    expect(client.acceptanceSignoffs.pending(request.requirement_id, request.commit)).toBeUndefined();
  }
});

it('definitive conflicts release the pending declaration while retaining it in other credentials', async () => {
  const client = new ApiClient('owner');
  const other = new ApiClient('other-owner-session');
  vi.spyOn(client, 'write').mockRejectedValue(new ApiError('version_conflict', '版本已变化', 409));
  other.acceptanceSignoffs.begin(coverage, request);
  await expect(submitSignoff(client, coverage, request)).rejects.toMatchObject({ code: 'version_conflict' });
  expect(client.acceptanceSignoffs.pending(request.requirement_id, request.commit)).toBeUndefined();
  expect(other.acceptanceSignoffs.pending(request.requirement_id, request.commit)?.body).toEqual(request);
});

it('rejects green declarations with missing manual confirmations or a different fixed version', () => {
  for (const value of [{ ...signedStatus, receipt: { ...receipt, manual_criteria: [] } },
    { ...signedStatus, receipt: { ...receipt, revision: 2 } }, { ...signedStatus, digest: 'e'.repeat(64) },
    { ...signedStatus, receipt: { ...receipt, state: 'revoked' } }]) {
    expect(() => signoffStatus(value, coverage)).toThrow('验收记录与当前覆盖不一致');
  }
});
