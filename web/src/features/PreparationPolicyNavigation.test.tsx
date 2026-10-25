import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import { ApiClient, ApiError } from '../api/client';
import { preparationSnapshot as snapshot } from '../test/preparation';
import { ProjectInitialization } from './ProjectInitialization';

const checks = [{ name: '验证', command: 'go', args: ['test', './...'], timeout_seconds: 30 }];
const input = JSON.stringify(checks, null, 2);
const initial = { ...snapshot, project: { ...snapshot.project!, checks: [] } };
const receipt = { ...initial.project, version: 2, policy_version: 2, checks };

it('preserves invalid unsent policy JSON and its version when leaving and returning', () => {
  const client = new ApiClient('owner');
  const write = vi.spyOn(client, 'write');
  const props = { client, refresh: vi.fn() };
  const view = render(<ProjectInitialization {...props} snapshot={initial} />);
  fireEvent.change(screen.getByLabelText(/实际检查 JSON/), { target: { value: '{unfinished' } });
  view.unmount();
  render(<ProjectInitialization {...props} snapshot={{ ...initial, project: { ...initial.project, version: 3 } }} />);
  expect(screen.getByLabelText(/实际检查 JSON/)).toHaveProperty('value', '{unfinished');
  fireEvent.click(screen.getByRole('button', { name: '保存 Owner 检查政策' }));
  expect(write).not.toHaveBeenCalled();
  expect(screen.getByRole('alert').textContent).toContain('政策版本已变化');
});

it.each([
  new ApiError('network_error', '回执丢失'),
  new ApiError('request_failed', '暂时失败', 503),
  new ApiError('invalid_response', '回执损坏', 200),
  {},
])('retains the exact pending policy after navigation and a newer snapshot: %s', async (failure) => {
  const client = new ApiClient('owner');
  const write = vi.spyOn(client, 'write');
  if (failure instanceof Error) write.mockRejectedValueOnce(failure);
  else write.mockResolvedValueOnce(failure);
  write.mockResolvedValueOnce(receipt);
  const props = { client, refresh: vi.fn() };
  const view = render(<ProjectInitialization {...props} snapshot={initial} />);
  fireEvent.change(screen.getByLabelText(/实际检查 JSON/), { target: { value: input } });
  fireEvent.click(screen.getByRole('button', { name: '保存 Owner 检查政策' }));
  await screen.findByRole('alert');
  const original = write.mock.calls[0];
  view.unmount();
  render(<ProjectInitialization {...props} snapshot={{ ...initial, project: {
    ...initial.project, version: 3, protected_paths: ['changed/**'],
  } }} />);
  expect(screen.getByLabelText(/实际检查 JSON/)).toHaveProperty('value', input);
  expect(screen.getByLabelText(/实际检查 JSON/)).toHaveProperty('disabled', true);
  fireEvent.click(screen.getByRole('button', { name: '重试待确认政策' }));
  await waitFor(() => expect(write).toHaveBeenCalledTimes(2));
  expect(write.mock.calls[1]).toEqual(original);
  expect(original).toEqual(['/project/policy', {
    expected_version: 1, checks, protected_paths: initial.project.protected_paths,
  }, expect.any(String)]);
  await waitFor(() => expect(client.initializationPolicies.read(initial.project.id)).toBeUndefined());
});

it('settles an accepted in-flight policy after route unmount without leaving a duplicate draft', async () => {
  const client = new ApiClient('owner');
  let accept!: () => void;
  const pending = new Promise((resolve) => { accept = () => resolve(receipt); });
  const write = vi.spyOn(client, 'write').mockReturnValue(pending);
  const props = { client, refresh: vi.fn(), snapshot: initial };
  const view = render(<ProjectInitialization {...props} />);
  fireEvent.change(screen.getByLabelText(/实际检查 JSON/), { target: { value: input } });
  fireEvent.click(screen.getByRole('button', { name: '保存 Owner 检查政策' }));
  await waitFor(() => expect(write).toHaveBeenCalledTimes(1));
  view.unmount();
  render(<ProjectInitialization {...props} />);
  await act(async () => { accept(); await pending; });
  await waitFor(() => expect(client.initializationPolicies.read(initial.project.id)).toBeUndefined());
  expect(screen.queryByRole('button', { name: '重试待确认政策' })).toBeNull();
  expect(write).toHaveBeenCalledTimes(1);
});

it('keeps a definitively rejected policy editable after navigation', async () => {
  const client = new ApiClient('owner');
  vi.spyOn(client, 'write').mockRejectedValue(new ApiError('version_conflict', '请核对政策版本', 409));
  const props = { client, refresh: vi.fn(), snapshot: initial };
  const view = render(<ProjectInitialization {...props} />);
  fireEvent.change(screen.getByLabelText(/实际检查 JSON/), { target: { value: input } });
  fireEvent.click(screen.getByRole('button', { name: '保存 Owner 检查政策' }));
  await screen.findByText('请核对政策版本');
  view.unmount();
  render(<ProjectInitialization {...props} />);
  expect(screen.getByLabelText(/实际检查 JSON/)).toHaveProperty('value', input);
  expect(screen.getByLabelText(/实际检查 JSON/)).toHaveProperty('disabled', false);
  expect(screen.queryByRole('button', { name: '重试待确认政策' })).toBeNull();
});
