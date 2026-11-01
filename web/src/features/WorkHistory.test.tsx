import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import { WorkHistory } from './WorkHistory';
import { ApiClient } from '../api/client';
import type { WorkContext, WorkNoteInput } from '../api.generated';

const anchor = { kind: 'task', id: 'task_1' };
const context: WorkContext = {
  anchor, queried_at: '2026-11-01T00:00:00Z', next_cursor: '', next_reference_cursor: '', notes: [], references: [],
  current: { reference: { ...anchor, version: 1, stamp: 'a'.repeat(64) }, available: true, stale: false, title: 'Task', text: '', state: 'running', truncated: false, questions: [], next_action: 'view_task' },
};
it('loads only when opened and saves discussion without execution or requirement operations', async () => {
  const client = new ApiClient('owner');
  const query = vi.spyOn(client, 'query').mockResolvedValue(context);
  const write = vi.spyOn(client, 'write').mockImplementation(async (_path, input) => {
    const body = input as WorkNoteInput;
    return { disposition: 'recorded_only', note: {
      ...body, id: 'note_1', sequence: 1, redacted: false, original_text_sha256: 'b'.repeat(64), anchor_stamp: body.expected_stamp,
      project_id: 'project', author_id: 'owner', chat_scope: 'owner', historical: false, created_at: '2026-11-01T00:00:00Z',
      source: { ...body.source, client_id: 'owner' },
    } } as never;
  });
  render(<WorkHistory client={client} anchor={anchor} />);
  expect(query).not.toHaveBeenCalled();
  fireEvent.click(screen.getByText('工作记录'));
  expect(await screen.findByText('还没有保存讨论。')).toBeTruthy();
  fireEvent.change(screen.getByLabelText('保存讨论'), { target: { value: '验收建议先讨论，不直接修改' } });
  fireEvent.click(screen.getByRole('button', { name: '保存讨论' }));
  expect(await screen.findByText('讨论已保存。')).toBeTruthy();
  expect(write).toHaveBeenCalledTimes(1); expect(write.mock.calls[0][0]).toBe('/work/notes');
  expect((write.mock.calls[0][1] as WorkNoteInput).expected_stamp).toBe(context.current.reference.stamp);
  expect((screen.getByLabelText('保存讨论') as HTMLTextAreaElement).value).toBe('');
});
it('does not silently rebind drafts on refresh or lose uncertain submissions on remount', async () => {
  const client = new ApiClient('owner');
  const query = vi.spyOn(client, 'query').mockResolvedValue(context);
  vi.spyOn(client, 'write').mockRejectedValue(new Error('offline'));
  const view = render(<WorkHistory client={client} anchor={anchor} />);
  fireEvent.click(screen.getByText('工作记录'));
  await screen.findByText('还没有保存讨论。');
  fireEvent.change(screen.getByLabelText('保存讨论'), { target: { value: '<script>literal discussion</script>' } });
  query.mockResolvedValue({ ...context, current: { ...context.current, reference: { ...context.current.reference, stamp: 'c'.repeat(64) } } });
  fireEvent.click(screen.getByRole('button', { name: '刷新当前事实' }));
  expect(await screen.findByText('核对后采用当前版本')).toBeTruthy();
  expect((screen.getByRole('button', { name: '保存讨论' }) as HTMLButtonElement).disabled).toBe(true);
  fireEvent.click(screen.getByRole('button', { name: '核对后采用当前版本' }));
  fireEvent.click(screen.getByRole('button', { name: '保存讨论' }));
  await screen.findByText('offline');
  view.unmount(); render(<WorkHistory client={client} anchor={anchor} />);
  fireEvent.click(screen.getByText('工作记录'));
  await waitFor(() => expect((screen.getByLabelText('保存讨论') as HTMLTextAreaElement).disabled).toBe(true));
  expect((screen.getByLabelText('保存讨论') as HTMLTextAreaElement).value).toBe('<script>literal discussion</script>');
  expect(screen.getByRole('button', { name: '重试确认保存' })).toBeTruthy();
});
