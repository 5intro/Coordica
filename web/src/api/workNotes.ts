import type { WorkAnchor, WorkNoteInput, WorkNoteReceipt } from '../api.generated';

type Writer = { write<T>(path: string, body: unknown, key?: string): Promise<T> };
export type WorkDraft = { text: string; stamp: string; request?: { body: WorkNoteInput; key: string } };

// Per-credential ApiClient memory survives route changes, never browser reloads
// or other identities. A missing receipt freezes the exact original envelope.
export class WorkNotes {
  private readonly drafts = new Map<string, WorkDraft>();
  get(anchor: WorkAnchor) { return this.drafts.get(`${anchor.kind}:${anchor.id}`); }
  edit(anchor: WorkAnchor, text: string, stamp: string) {
    const prior = this.get(anchor);
    if (!prior?.request) this.drafts.set(`${anchor.kind}:${anchor.id}`, { text, stamp: prior?.stamp ?? stamp });
  }
  adopt(anchor: WorkAnchor, stamp: string) {
    const draft = this.get(anchor);
    if (draft && !draft.request) draft.stamp = stamp;
  }
  async save(api: Writer, anchor: WorkAnchor): Promise<WorkNoteReceipt> {
    const draft = this.get(anchor);
    if (!draft?.text.trim()) throw new Error('请先填写要保存的讨论。');
    if (new TextEncoder().encode(draft.text).length > 16384) throw new Error('讨论超过 16 KiB，请分段保存。');
    draft.request ??= {
      key: crypto.randomUUID(),
      body: {
        anchor: { ...anchor }, expected_stamp: draft.stamp, kind: 'note', original_text: draft.text, references: [],
        source: { session_id: 'web_work', message_id: crypto.randomUUID() },
      },
    };
    const { body, key } = draft.request;
    let receipt: WorkNoteReceipt;
    try { receipt = await api.write<WorkNoteReceipt>('/work/notes', body, key); }
    catch (error) {
      const status = (error as { status?: number } | null)?.status;
      if (status && [400, 401, 403, 404, 409, 413, 422].includes(status)) draft.request = undefined;
      throw error;
    }
    const note = receipt?.note;
    if (receipt?.disposition !== 'recorded_only' || !note?.id || note.kind !== 'note' || note.redacted !== false
      || !note.project_id || !note.author_id || note.source?.client_id !== note.author_id
      || note.chat_scope !== 'owner' || note.historical !== false || !Number.isFinite(Date.parse(note.created_at))
      || !/^[a-f0-9]{64}$/.test(note.original_text_sha256) || !Number.isInteger(note.sequence) || note.sequence < 1
      || note.anchor?.kind !== anchor.kind || note.anchor?.id !== anchor.id || note.anchor_stamp !== body.expected_stamp
      || note.original_text !== body.original_text || (note.assistant_summary ?? '') !== '' || note.references?.length !== 0 || note.corrects
      || note.source?.session_id !== body.source.session_id || note.source?.message_id !== body.source.message_id) {
      throw new Error('保存回执未确认。原文和请求已保留，请重试核对。');
    }
    this.drafts.delete(`${anchor.kind}:${anchor.id}`);
    return receipt;
  }
}
