import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import { ApiClient, ApiError } from '../../api/client';
import { signoffCoverage as coverage, signoffReceipt as receipt, signedStatus, unsignedStatus } from '../../test/acceptanceSignoff';
import { CoverageDetails } from './CoverageDetails';
import { RequirementAcceptance } from './RequirementAcceptance';
import { centerSnapshot } from '../../test/connectionInfo';

it('requires every manual clause and a note, then displays current-version human acceptance', async () => {
  const client = new ApiClient('owner');
  vi.spyOn(client, 'read').mockResolvedValueOnce(unsignedStatus).mockResolvedValue(signedStatus);
  const write = vi.spyOn(client, 'write').mockResolvedValue(receipt);
  render(<CoverageDetails coverage={coverage} client={client} refresh={vi.fn()} />);
  fireEvent.click(await screen.findByRole('button', { name: '记录验收' }));
  const submit = screen.getByRole('button', { name: '签收当前版本' });
  expect(submit).toHaveProperty('disabled', true);
  fireEvent.change(screen.getByLabelText('验收说明'), { target: { value: receipt.note } });
  expect(submit).toHaveProperty('disabled', true);
  fireEvent.click(screen.getByRole('checkbox', { name: '支付结果清晰' }));
  fireEvent.click(submit);
  expect(await screen.findByText('当前版本已验收')).toBeTruthy();
  expect(screen.getByText('已人工确认')).toBeTruthy();
  expect(screen.queryByRole('region', { name: '补测计划预览' })).toBeNull();
  expect(write.mock.calls[0]).toEqual(['/tests/acceptance/signoff', {
    requirement_id: 'req_pay', commit: coverage.commit, action: 'accept', expected_version: 0,
    expected_digest: coverage.digest, manual_criteria: ['experience'], note: receipt.note,
  }, expect.any(String)]);
});

it('revokes without re-confirming manual clauses and preserves the original declaration', async () => {
  const client = new ApiClient('owner');
  const revoked = { ...receipt, version: 2, state: 'revoked', note: '需要重新体验' };
  vi.spyOn(client, 'read').mockResolvedValueOnce(signedStatus).mockResolvedValue({ ...unsignedStatus,
    receipt: revoked, recent: [revoked, receipt] });
  const write = vi.spyOn(client, 'write').mockResolvedValue(revoked);
  render(<CoverageDetails coverage={coverage} client={client} refresh={vi.fn()} />);
  fireEvent.click(await screen.findByRole('button', { name: '撤销验收' }));
  fireEvent.change(screen.getByLabelText('撤销原因'), { target: { value: revoked.note } });
  fireEvent.click(screen.getByRole('button', { name: '确认撤销' }));
  expect(await screen.findByText('已撤销验收')).toBeTruthy();
  expect(screen.queryByText('当前版本已验收')).toBeNull();
  expect(write.mock.calls[0][1]).toEqual({ requirement_id: 'req_pay', commit: coverage.commit, action: 'revoke',
    expected_version: 1, expected_digest: '', manual_criteria: [], note: revoked.note });
  fireEvent.click(screen.getByText('最近验收记录'));
  expect(screen.getByText(receipt.note)).toBeTruthy();
});

it('retains an uncertain original declaration across remount and changed coverage without signing the new view', async () => {
  const client = new ApiClient('owner');
  const read = vi.spyOn(client, 'read').mockResolvedValue(unsignedStatus);
  const write = vi.spyOn(client, 'write').mockRejectedValueOnce(new ApiError('network_error', '连接中断'))
    .mockResolvedValue(receipt);
  const first = render(<CoverageDetails coverage={coverage} client={client} refresh={vi.fn()} />);
  fireEvent.click(await screen.findByRole('button', { name: '记录验收' }));
  fireEvent.change(screen.getByLabelText('验收说明'), { target: { value: receipt.note } });
  fireEvent.click(screen.getByRole('checkbox', { name: '支付结果清晰' }));
  fireEvent.click(screen.getByRole('button', { name: '签收当前版本' }));
  await screen.findByText('连接中断');
  first.unmount();
  const next = { ...coverage, digest: 'c'.repeat(64), plan: { ...coverage.plan, version: 3 } };
  read.mockResolvedValue({ ...unsignedStatus, digest: next.digest, receipt, recent: [receipt] });
  render(<CoverageDetails coverage={next} client={client} refresh={vi.fn()} />);
  expect(screen.getByLabelText('验收说明')).toHaveProperty('value', receipt.note);
  expect(screen.getByLabelText('验收说明')).toHaveProperty('disabled', true);
  fireEvent.click(screen.getByRole('button', { name: '确认原提交结果' }));
  await waitFor(() => expect(write).toHaveBeenCalledTimes(2));
  expect(write.mock.calls[1]).toEqual(write.mock.calls[0]);
  await screen.findByText('原验收已过期');
  expect(screen.queryByText('当前版本已验收')).toBeNull();
});

it('rejects a false green response when automated evidence is missing', async () => {
  const client = new ApiClient('owner');
  vi.spyOn(client, 'read').mockResolvedValue(signedStatus);
  const failed = { ...coverage, criteria: coverage.criteria!.map((v) => v.criterion.method === 'automated' ? { ...v, state: 'test_gap' } : v) };
  render(<CoverageDetails coverage={failed} client={client} refresh={vi.fn()} />);
  await screen.findByText('验收记录与当前覆盖不一致，请重新核对。');
  expect(screen.queryByText('当前版本已验收')).toBeNull();
  expect(screen.getByRole('button', { name: '记录验收' })).toHaveProperty('disabled', true);
});

it('allows querying and revoking an old signoff after withdrawal while refusing new acceptance', async () => {
  const client = new ApiClient('owner');
  const stale = { ...coverage, current: false, digest: 'd'.repeat(64),
    requirement: { ...coverage.requirement, state: 'withdrawn', version: 3 } };
  let status = { ...unsignedStatus, digest: stale.digest, can_accept: false, receipt, recent: [receipt] };
  vi.spyOn(client, 'read').mockImplementation(async (path) => path.includes('/signoff') ? status
    : path.includes('/plan') ? coverage.plan : stale);
  const write = vi.spyOn(client, 'write').mockImplementation(async () => {
    const revoked = { ...receipt, version: 2, state: 'revoked', note: '需求已撤回' };
    status = { ...status, receipt: revoked, recent: [revoked, receipt] };
    return revoked;
  });
  render(<RequirementAcceptance snapshot={{ ...centerSnapshot, requirements: [stale.requirement] }} client={client} refresh={vi.fn()} />);
  fireEvent.change(screen.getByLabelText('关联需求'), { target: { value: 'req_pay' } });
  await screen.findByRole('button', { name: '编辑验收条款' });
  fireEvent.change(screen.getByLabelText('验收固定提交'), { target: { value: coverage.commit } });
  fireEvent.click(screen.getByRole('button', { name: '检查覆盖并预览补测' }));
  fireEvent.click(await screen.findByRole('button', { name: '撤销验收' }));
  expect(screen.queryByText('当前版本已验收')).toBeNull();
  fireEvent.change(screen.getByLabelText('撤销原因'), { target: { value: '需求已撤回' } });
  fireEvent.click(screen.getByRole('button', { name: '确认撤销' }));
  await screen.findByText('已撤销验收');
  expect(write.mock.calls[0][1]).toMatchObject({ action: 'revoke', expected_version: 1 });
  expect(screen.getByRole('button', { name: '记录验收' })).toHaveProperty('disabled', true);
});
