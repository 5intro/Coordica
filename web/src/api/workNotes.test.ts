import { expect, it, vi } from 'vitest';
import { WorkNotes } from './workNotes';
import { ApiError } from './client';
import type { WorkAnchor, WorkNoteInput } from '../api.generated';

const anchor: WorkAnchor = { kind: 'task', id: 'task_1' };
const stamp = 'a'.repeat(64);
function receipt(body: WorkNoteInput) {
  return { disposition: 'recorded_only', note: {
    ...body, id: 'note_1', sequence: 1, redacted: false, original_text_sha256: 'b'.repeat(64), anchor_stamp: body.expected_stamp,
    project_id: 'project', author_id: 'owner', chat_scope: 'owner', historical: false, created_at: '2026-11-01T00:00:00Z',
    source: { ...body.source, client_id: 'owner' },
  } };
}
it('retains the exact unknown request across routes and rejects a malformed receipt', async () => {
  const notes = new WorkNotes(); notes.edit(anchor, '原话 <script> only prose', stamp);
  const write = vi.fn().mockRejectedValueOnce(new ApiError('network_error', 'offline'))
    .mockResolvedValueOnce({ disposition: 'recorded_only', note: { id: 'incomplete' } })
    .mockImplementationOnce(async (_path, body) => receipt(body));
  await expect(notes.save({ write }, anchor)).rejects.toThrow('offline');
  const request = structuredClone(notes.get(anchor)?.request);
  notes.edit(anchor, 'must not overwrite', 'new stamp'); notes.adopt(anchor, 'new stamp');
  await expect(notes.save({ write }, anchor)).rejects.toThrow('回执未确认');
  expect(notes.get(anchor)?.request).toEqual(request);
  await notes.save({ write }, anchor);
  expect(write.mock.calls[0]).toEqual(write.mock.calls[1]); expect(write.mock.calls[1]).toEqual(write.mock.calls[2]);
  expect(notes.get(anchor)).toBeUndefined();
});
it('retains rejected original text and version until explicit adoption, without crossing identity or anchor', async () => {
  const notes = new WorkNotes(); notes.edit(anchor, 'unchanged original', stamp);
  const write = vi.fn().mockRejectedValue(new ApiError('version_conflict', 'refresh', 409));
  await expect(notes.save({ write }, anchor)).rejects.toThrow('refresh');
  expect(notes.get(anchor)).toEqual({ text: 'unchanged original', stamp, request: undefined });
  notes.edit(anchor, 'edit draft', 'new'); expect(notes.get(anchor)?.stamp).toBe(stamp);
  notes.adopt(anchor, 'new'); expect(notes.get(anchor)?.stamp).toBe('new');
  expect(notes.get({ kind: 'requirement', id: anchor.id })).toBeUndefined();
  expect(new WorkNotes().get(anchor)).toBeUndefined();
});

it('checks the UTF-8 byte bound before creating a request', async () => {
  const notes = new WorkNotes(); notes.edit(anchor, '中'.repeat(6000), stamp);
  const write = vi.fn();
  await expect(notes.save({ write }, anchor)).rejects.toThrow('16 KiB');
  expect(write).not.toHaveBeenCalled(); expect(notes.get(anchor)?.request).toBeUndefined();
});
