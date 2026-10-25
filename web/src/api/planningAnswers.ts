import type { PlanningMessageRequest } from '../api.generated';

type Draft = { version: number; run: string; question: string; text: string };
export type PendingPlanningAnswer = { key: string; body: PlanningMessageRequest };
type State = { draft: Draft; pending: PendingPlanningAnswer | null };

// Private drafts and uncertain writes survive route changes only in the
// current credential's ApiClient. Nothing is copied to browser persistence.
export class PlanningAnswers {
  private readonly projects = new Map<string, State>();
  private readonly listeners = new Set<() => void>();
  read(project: string) { return this.projects.get(project); }
  subscribe = (listener: () => void) => {
    this.listeners.add(listener);
    return () => { this.listeners.delete(listener); };
  };
  edit(project: string, draft: Draft) {
    if (this.read(project)?.pending) return;
    this.projects.set(project, { draft, pending: null });
    this.notify();
  }
  begin(project: string, request: PendingPlanningAnswer) {
    const existing = this.read(project)?.pending;
    if (existing) return existing;
    this.projects.set(project, { pending: request, draft: {
      version: request.body.expected_version, run: request.body.reply_to_run_id!,
      question: request.body.question!, text: request.body.text,
    } });
    this.notify();
    return request;
  }
  settle(project: string, key: string, accepted: boolean) {
    const state = this.read(project);
    if (state?.pending?.key !== key) return;
    if (accepted) this.projects.delete(project);
    else this.projects.set(project, { ...state, pending: null });
    this.notify();
  }
  private notify() { this.listeners.forEach((listener) => listener()); }
}
