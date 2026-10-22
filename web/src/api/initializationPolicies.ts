import type { Project, ProjectPolicyRequest } from '../api.generated';

export type PolicyDraft = { version: number; checks: string; protectedPaths: Project['protected_paths'] };
export type PendingPolicy = { key: string; body: ProjectPolicyRequest };
type PolicyState = { draft: PolicyDraft; pending: PendingPolicy | null };

export function projectPolicyDraft(project: Project): PolicyDraft {
  return { version: project.version, checks: JSON.stringify(project.checks ?? [], null, 2),
    protectedPaths: project.protected_paths ? [...project.protected_paths] : null };
}

// Policy input is private to this ApiClient credential and project. Keep both
// editable JSON and the exact uncertain envelope through route unmounts.
export class InitializationPolicies {
  private readonly projects = new Map<string, PolicyState>();
  private readonly listeners = new Set<() => void>();
  read(projectID: string) { return this.projects.get(projectID); }
  subscribe = (listener: () => void) => {
    this.listeners.add(listener);
    return () => { this.listeners.delete(listener); };
  };
  private notify() { this.listeners.forEach((listener) => listener()); }
  edit(projectID: string, draft: PolicyDraft) {
    if (this.read(projectID)?.pending) return;
    this.projects.set(projectID, { draft, pending: null });
    this.notify();
  }
  begin(projectID: string, draft: PolicyDraft, request: PendingPolicy): PendingPolicy {
    const pending = this.read(projectID)?.pending;
    if (pending) return pending;
    this.projects.set(projectID, { draft, pending: request });
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
