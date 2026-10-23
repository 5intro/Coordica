import type { InitializationAnswer } from '../api.generated';

export type AnswerDraft = { version: number; question: string; text: string };
export type PendingAnswer = { key: string; body: InitializationAnswer };
type AnswerState = { draft: AnswerDraft; pending: PendingAnswer | null };

// Original prose is kept only in this credential's memory, isolated by project.
// A route remount must not lose the envelope of a write with an unknown outcome.
export class InitializationAnswers {
  private readonly projects = new Map<string, AnswerState>();
  private readonly listeners = new Set<() => void>();

  read(projectID: string) { return this.projects.get(projectID); }
  subscribe = (listener: () => void) => {
    this.listeners.add(listener);
    return () => { this.listeners.delete(listener); };
  };
  private notify() { this.listeners.forEach((listener) => listener()); }

  edit(projectID: string, draft: AnswerDraft) {
    if (this.read(projectID)?.pending) return;
    this.projects.set(projectID, { draft, pending: null });
    this.notify();
  }
  begin(projectID: string, request: PendingAnswer): PendingAnswer {
    const existing = this.read(projectID)?.pending;
    if (existing) return existing;
    this.projects.set(projectID, { pending: request, draft: {
      version: request.body.expected_version, question: request.body.question_id, text: request.body.original_text,
    } });
    this.notify();
    return request;
  }
  settle(projectID: string, key: string, accepted: boolean) {
    const state = this.read(projectID);
    if (state?.pending?.key !== key) return;
    if (accepted) this.projects.delete(projectID);
    else this.projects.set(projectID, { draft: state.draft, pending: null });
    this.notify();
  }
}
