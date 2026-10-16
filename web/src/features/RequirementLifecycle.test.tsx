import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import type { Requirement, RequirementDetail } from '../api.generated';
import { ApiClient, ApiError } from '../api/client';
import { RequirementLifecycle } from './RequirementLifecycle';

const requirement: Requirement = { id: 'req_test', version: 4, content_revision: 2, state: 'active', title: 'Export reports', body: 'Use a stable format', original_text: 'Initial prose', created_at: '' };
function fixture() {
  const client = new ApiClient('owner');
  const detail: RequirementDetail = { requirement, revisions: [{ requirement, reason: 'Format changed', actor_id: 'owner', created_at: '' }], changes: [], impact: [], before: 0 };
  vi.spyOn(client, 'read').mockResolvedValue(detail);
  return { client, detail, refresh: vi.fn() };
}
it('pins the original version, preserves a rejected draft, and submits only the formal revision fields', async () => {
  const { client, refresh } = fixture();
  const write = vi.spyOn(client, 'write').mockRejectedValueOnce(new ApiError('version_conflict', '需求已有新版本')).mockResolvedValueOnce({});
  render(<RequirementLifecycle requirement={requirement} client={client} refresh={refresh}/>);
  fireEvent.click(screen.getByRole('button', { name: '修订需求' }));
  fireEvent.change(screen.getByLabelText('修订标题'), { target: { value: 'Export signed reports' } });
  fireEvent.change(screen.getByLabelText('修订原因'), { target: { value: 'Audit needs' } });
  await waitFor(() => expect((screen.getByRole('button', { name: '保存修订' }) as HTMLButtonElement).disabled).toBe(false));
  fireEvent.click(screen.getByRole('button', { name: '保存修订' }));
  expect(await screen.findByRole('alert')).toHaveProperty('textContent', '需求已有新版本');
  expect((screen.getByLabelText('修订标题') as HTMLInputElement).value).toBe('Export signed reports');
  expect(write).toHaveBeenCalledWith('/requirements/req_test/revise', { expected_version: 4, title: 'Export signed reports', body: 'Use a stable format', reason: 'Audit needs' });
  await waitFor(() => expect((screen.getByRole('button', { name: '保存修订' }) as HTMLButtonElement).disabled).toBe(false));
  fireEvent.click(screen.getByRole('button', { name: '保存修订' }));
  await waitFor(() => expect(screen.queryByRole('form')).toBeNull());
});
it('requires an explicit reason and impact receipt before withdrawal, keeping code/history handling visible', async () => {
  const { client, refresh } = fixture();const write = vi.spyOn(client, 'write').mockResolvedValue({});
  render(<RequirementLifecycle requirement={requirement} client={client} refresh={refresh}/>);
  fireEvent.click(screen.getByRole('button', { name: '撤回需求' }));
  expect(screen.getByText(/已合并代码通过补偿任务处理/)).toBeTruthy();
  expect((screen.getByRole('button', { name: '确认撤回' }) as HTMLButtonElement).disabled).toBe(true);
  fireEvent.change(screen.getByLabelText('撤回原因'), { target: { value: 'No longer required' } });
  await waitFor(() => expect((screen.getByRole('button', { name: '确认撤回' }) as HTMLButtonElement).disabled).toBe(false));
  fireEvent.click(screen.getByRole('button', { name: '确认撤回' }));
  await waitFor(() => expect(write).toHaveBeenCalledWith('/requirements/req_test/withdraw', { expected_version: 4, reason: 'No longer required' }));
});
it('shows history and a stopped blocked change retry without enabling automation', async () => {
  const { client, detail, refresh } = fixture();
  detail.changes = [{ id: 'change', version: 3, requirement_id: requirement.id, content_revision: 2, kind: 'revise', state: 'blocked', reason: 'Change', goal_version: 1, goal_authorization_version: 1, automation_version: 1, impacts: [], task_ids: [], detail: 'Scope needs review', created_at: '' }];
  const write = vi.spyOn(client, 'write').mockResolvedValue({});
  render(<RequirementLifecycle requirement={{ ...requirement, state: 'withdrawn' }} client={client} refresh={refresh}/>);
  fireEvent.click(screen.getByRole('button', { name: '版本历史' }));
  expect(await screen.findByText(/需要处理 · Scope needs review/)).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: '重新评估变更' }));
  await waitFor(() => expect(write).toHaveBeenCalledWith('/requirements/req_test/reassess', { expected_version: 3 }));
  expect(write).toHaveBeenCalledTimes(1);
});
