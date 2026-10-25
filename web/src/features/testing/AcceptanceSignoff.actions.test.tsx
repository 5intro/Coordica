import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import { ApiClient } from '../../api/client';
import { signoffCoverage as coverage, signoffReceipt as receipt, signedStatus, unsignedStatus } from '../../test/acceptanceSignoff';
import { CoverageDetails } from './CoverageDetails';

it('signs ready coverage again directly when the previous accepted receipt is stale', async () => {
  const client = new ApiClient('owner');
  const next = { ...coverage, digest: 'c'.repeat(64), plan: { ...coverage.plan, version: 3 } };
  vi.spyOn(client, 'read').mockResolvedValue({ ...unsignedStatus, digest: next.digest, receipt, recent: [receipt] });
  const write = vi.spyOn(client, 'write').mockResolvedValue({ ...receipt, version: 2,
    coverage_digest: next.digest, plan_version: 3 });
  render(<CoverageDetails coverage={next} client={client} refresh={vi.fn()} />);
  fireEvent.click(await screen.findByRole('button', { name: '记录验收' }));
  fireEvent.change(screen.getByLabelText('验收说明'), { target: { value: receipt.note } });
  fireEvent.click(screen.getByRole('checkbox', { name: '支付结果清晰' }));
  fireEvent.click(screen.getByRole('button', { name: '签收当前版本' }));
  await waitFor(() => expect(write).toHaveBeenCalledTimes(1));
  expect(write.mock.calls[0][1]).toMatchObject({ action: 'accept', expected_version: 1,
    expected_digest: next.digest, manual_criteria: ['experience'] });
});

it('also permits revoking a stale receipt without signing ready coverage again', async () => {
  const client = new ApiClient('owner');
  vi.spyOn(client, 'read').mockResolvedValue({ ...unsignedStatus, receipt: { ...receipt, coverage_digest: 'c'.repeat(64) },
    recent: [] });
  const write = vi.spyOn(client, 'write').mockResolvedValue({ ...receipt, state: 'revoked', version: 2, note: '旧版本不再适用' });
  render(<CoverageDetails coverage={coverage} client={client} refresh={vi.fn()} />);
  fireEvent.click(await screen.findByRole('button', { name: '撤销旧验收' }));
  fireEvent.change(screen.getByLabelText('撤销原因'), { target: { value: '旧版本不再适用' } });
  fireEvent.click(screen.getByRole('button', { name: '确认撤销' }));
  await waitFor(() => expect(write).toHaveBeenCalledTimes(1));
  expect(write.mock.calls[0][1]).toMatchObject({ action: 'revoke', expected_version: 1 });
});

it('requires fresh manual confirmation when coverage changes while a declaration is being drafted', async () => {
  const client = new ApiClient('owner');
  const read = vi.spyOn(client, 'read').mockResolvedValue(unsignedStatus);
  const refresh = vi.fn();
  const page = render(<CoverageDetails coverage={coverage} client={client} refresh={refresh} />);
  fireEvent.click(await screen.findByRole('button', { name: '记录验收' }));
  fireEvent.change(screen.getByLabelText('验收说明'), { target: { value: receipt.note } });
  fireEvent.click(screen.getByRole('checkbox', { name: '支付结果清晰' }));
  expect(screen.getByRole('button', { name: '签收当前版本' })).toHaveProperty('disabled', false);
  const next = { ...coverage, digest: 'c'.repeat(64), plan: { ...coverage.plan, version: 3 } };
  read.mockResolvedValue({ ...unsignedStatus, digest: next.digest });
  page.rerender(<CoverageDetails coverage={next} client={client} refresh={refresh} />);
  await waitFor(() => expect(read).toHaveBeenCalledTimes(2));
  expect(screen.getByRole('checkbox', { name: '支付结果清晰' })).toHaveProperty('checked', false);
  expect(screen.getByLabelText('验收说明')).toHaveProperty('value', '');
  expect(screen.getByRole('button', { name: '签收当前版本' })).toHaveProperty('disabled', true);
});

it.each(['accept', 'revoke'])('enforces UTF-8 bytes for the %s note before submitting', async (action) => {
  const client = new ApiClient('owner');
  vi.spyOn(client, 'read').mockResolvedValue(action === 'accept' ? unsignedStatus : signedStatus);
  const write = vi.spyOn(client, 'write').mockRejectedValue(new Error('unexpected submission'));
  render(<CoverageDetails coverage={coverage} client={client} refresh={vi.fn()} />);
  fireEvent.click(await screen.findByRole('button', { name: action === 'accept' ? '记录验收' : '撤销验收' }));
  if (action === 'accept') fireEvent.click(screen.getByRole('checkbox', { name: '支付结果清晰' }));
  const note = screen.getByLabelText(action === 'accept' ? '验收说明' : '撤销原因');
  const submit = screen.getByRole('button', { name: action === 'accept' ? '签收当前版本' : '确认撤销' });
  fireEvent.change(note, { target: { value: '验'.repeat(1365) + 'a' } });
  expect(submit).toHaveProperty('disabled', false);
  fireEvent.change(note, { target: { value: '验'.repeat(1365) + 'ab' } });
  expect(submit).toHaveProperty('disabled', true);
  expect(screen.getByRole('alert').textContent).toContain('4096');
  fireEvent.submit(note.closest('form')!);
  expect(write).not.toHaveBeenCalled();
  fireEvent.change(note, { target: { value: '😀'.repeat(1024) } });
  expect(submit).toHaveProperty('disabled', false);
  fireEvent.change(note, { target: { value: '😀'.repeat(1024) + 'a' } });
  expect(submit).toHaveProperty('disabled', true);
});
