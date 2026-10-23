import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import { ApiClient } from '../api/client';
import { acceptedAnswer } from '../test/answerReceipt';
import { preparationSnapshot as snapshot } from '../test/preparation';
import { ProjectInitialization } from './ProjectInitialization';

it('retains a pending answer when a valid initialization receipt proves a different answer', async () => {
  const client = new ApiClient('owner');
  const write = vi.spyOn(client, 'write').mockImplementation(async (_path, body) => {
    const result = acceptedAnswer(body);
    return { ...result, feedback: snapshot.initialization!.feedback };
  });
  const props = { client, refresh: vi.fn(), snapshot };
  const view = render(<ProjectInitialization {...props} />);
  fireEvent.change(screen.getByLabelText('原始回答 / 反馈'), { target: { value: '保留完整原文' } });
  fireEvent.click(screen.getByRole('button', { name: '保存原文并继续访谈' }));
  await screen.findByRole('alert');
  const original = write.mock.calls[0];
  view.unmount();
  write.mockImplementation(async (_path, body) => acceptedAnswer(body));
  render(<ProjectInitialization {...props} />);
  fireEvent.click(screen.getByRole('button', { name: '重试待确认回答' }));
  await waitFor(() => expect(write).toHaveBeenCalledTimes(2));
  expect(write.mock.calls[1]).toEqual(original);
  await waitFor(() => expect(client.initializationAnswers.read(snapshot.project!.id)).toBeUndefined());
});

it.each(['checks', 'protected_paths', 'policy_version'])(
  'retains the original policy when a valid receipt mismatches %s', async (field) => {
    const client = new ApiClient('owner');
    const project = snapshot.project!;
    const checks = [{ name: 'test', command: 'go', args: ['test'], timeout_seconds: 30 }];
    const good = { ...project, version: project.version + 1, policy_version: project.policy_version + 1, checks };
    const changed = { checks: project.checks, protected_paths: [], policy_version: project.policy_version };
    const write = vi.spyOn(client, 'write').mockResolvedValueOnce({ ...good, [field]: changed[field as keyof typeof changed] })
      .mockResolvedValueOnce(good);
    const props = { client, refresh: vi.fn(), snapshot: { ...snapshot, project: { ...project, checks: [] } } };
    const view = render(<ProjectInitialization {...props} />);
    fireEvent.change(screen.getByLabelText(/实际检查 JSON/), { target: { value: JSON.stringify(checks) } });
    fireEvent.click(screen.getByRole('button', { name: '保存 Owner 检查政策' }));
    await screen.findByRole('alert');
    const original = write.mock.calls[0];
    view.unmount();
    render(<ProjectInitialization {...props} />);
    fireEvent.click(screen.getByRole('button', { name: '重试待确认政策' }));
    await waitFor(() => expect(write).toHaveBeenCalledTimes(2));
    expect(write.mock.calls[1]).toEqual(original);
    await waitFor(() => expect(client.initializationPolicies.read(project.id)).toBeUndefined());
  },
);
