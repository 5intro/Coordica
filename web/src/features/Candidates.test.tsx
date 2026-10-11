import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import type { Candidate, Snapshot } from '../api.generated';
import { ApiClient } from '../api/client';
import { Candidates } from './Candidates';

describe('protected candidate approval', () => {
  it('binds approval to the displayed fixed candidate and version', async () => {
    const candidate: Candidate = { id: 'candidate-1', version: 7, task_id: 'task-1', source_run_id: 'run-1', authorization_version: 2, policy_version: 1, base: 'base', head: 'head', main: 'main', commit: 'fixed-commit', state: 'ready_to_land', detail: '', protected: true, approved_commit: '', checks: null, created_at: '' };
    const snapshot: Snapshot = { api_version: 'v1', project: null, tasks: null, requirements: null, runs: null, candidates: [candidate], nodes: null, events: null };
    const client = new ApiClient('token');
    const write = vi.spyOn(client, 'write').mockResolvedValue({ ...candidate, approved_commit: candidate.commit });
    vi.spyOn(window, 'confirm').mockReturnValue(true);
    render(<Candidates snapshot={snapshot} client={client} refresh={() => undefined} />);
    fireEvent.click(screen.getByRole('button', { name: '特别批准此 C' }));
    await waitFor(() => expect(write).toHaveBeenCalledWith('/candidates/candidate-1/approve', { expected_version: 7 }));
    expect(window.confirm).toHaveBeenCalledWith(expect.stringContaining('fixed-commit'));
  });

  it('does not offer approval for an invalidated candidate', () => {
    const candidate = { id: 'candidate-1', version: 8, task_id: 'task-1', source_run_id: 'run-1', authorization_version: 2, policy_version: 1, base: 'base', head: 'head', main: 'main', commit: 'fixed-commit', state: 'invalidated', detail: '', protected: true, approved_commit: '', checks: null, created_at: '' };
    const snapshot: Snapshot = { api_version: 'v1', project: null, tasks: null, requirements: null, runs: null, candidates: [candidate], nodes: null, events: null };
    render(<Candidates snapshot={snapshot} client={new ApiClient('token')} refresh={() => undefined} />);
    expect(screen.queryByRole('button', { name: '特别批准此 C' })).toBeNull();
  });
});
