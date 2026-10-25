import { emptyPhaseB } from '../test/phaseB';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import type { Candidate, Snapshot } from '../api.generated';
import { ApiClient } from '../api/client';
import { localTask } from '../test/nodeSnapshot';
import { Candidates } from './Candidates';

describe('protected candidate approval', () => {
  it('binds approval to the displayed fixed candidate and version', async () => {
    const candidate: Candidate = { baseline_version: 1, baseline_digest: 'fixture', id: 'candidate-1', version: 7, task_id: 'task-1', source_run_id: 'run-1', authorization_version: 2, policy_version: 1, base: 'base', head: 'head', main: 'main', commit: 'fixed-commit', state: 'ready_to_land', detail: '', protected: true, approved_commit: '', checks: null, created_at: '' };
    const snapshot: Snapshot = { ...emptyPhaseB, api_version: 'v1', project: null, tasks: null, requirements: null, runs: null, candidates: [candidate], nodes: null, events: null };
    const client = new ApiClient('token');
    const write = vi.spyOn(client, 'write').mockResolvedValue({ ...candidate, approved_commit: candidate.commit });
    vi.spyOn(window, 'confirm').mockReturnValue(true);
    render(<Candidates snapshot={snapshot} client={client} refresh={() => undefined} />);
    fireEvent.click(screen.getByRole('button', { name: '特别批准此 C' }));
    await waitFor(() => expect(write).toHaveBeenCalledWith('/candidates/candidate-1/approve', { expected_version: 7 }));
    expect(window.confirm).toHaveBeenCalledWith(expect.stringContaining('fixed-commit'));
  });

  it('does not offer approval for an invalidated candidate', () => {
    const candidate = { baseline_version: 1, baseline_digest: 'fixture', id: 'candidate-1', version: 8, task_id: 'task-1', source_run_id: 'run-1', authorization_version: 2, policy_version: 1, base: 'base', head: 'head', main: 'main', commit: 'fixed-commit', state: 'invalidated', detail: '', protected: true, approved_commit: '', checks: null, created_at: '' };
    const snapshot: Snapshot = { ...emptyPhaseB, api_version: 'v1', project: null, tasks: null, requirements: null, runs: null, candidates: [candidate], nodes: null, events: null };
    render(<Candidates snapshot={snapshot} client={new ApiClient('token')} refresh={() => undefined} />);
    expect(screen.queryByRole('button', { name: '特别批准此 C' })).toBeNull();
  });
});

const pending: Candidate = { baseline_version: 1, baseline_digest: 'fixture', id: 'pending', version: 1, task_id: 'task-1', source_run_id: 'run-1', authorization_version: 1, policy_version: 1, base: 'base', head: 'head', main: '', commit: '', state: 'accepted', detail: '', protected: false, approved_commit: '', checks: null, created_at: '' };
const activeStates = ['accepted', 'prechecking', 'reviewing', 'validating', 'ready_to_land', 'landing', 'reconciling'];
const historyStates = ['failed', 'landed', 'rejected', 'invalidated', 'cancelled'];
function renderCandidates(candidates: Candidate[]) {
  const snapshot: Snapshot = { ...emptyPhaseB, api_version: 'v1', project: null, tasks: null, requirements: null, runs: null, candidates, nodes: null, events: null };
  const props = { snapshot, client: new ApiClient('token'), refresh: () => undefined };
  return { ...render(<Candidates {...props} />), props };
}

describe('candidate progress visibility', () => {
  it.each(activeStates)('shows %s in progress without requiring a fixed commit', (state) => {
    renderCandidates([{ ...pending, state }]);
    expect(within(screen.getByRole('region', { name: '候选列表' })).getByRole('button')).toBeTruthy();
    expect(screen.getByText('固定候选尚未生成')).toBeTruthy();
    expect(screen.getByText(/进行中包含候选准备/)).toBeTruthy();
    expect(screen.queryByRole('button', { name: '特别批准此 C' })).toBeNull();
  });

  it('keeps the same candidate visible throughout preparation, review, integration and reconciliation', () => {
    const { rerender, props } = renderCandidates([pending]);
    for (const state of activeStates) {
      rerender(<Candidates {...props} snapshot={{ ...props.snapshot, candidates: [{ ...pending, state, commit: state === 'accepted' ? '' : 'fixed' }] }} />);
      expect(within(screen.getByRole('region', { name: '候选列表' })).getByRole('button').getAttribute('aria-pressed')).toBe('true');
    }
    expect(screen.getByText(/正在核对集成结果/)).toBeTruthy();
    rerender(<Candidates {...props} snapshot={{ ...props.snapshot, candidates: [{ ...pending, state: 'landed', commit: 'fixed' }] }} />);
    expect(within(screen.getByRole('region', { name: '候选列表' })).queryByRole('button')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: '历史' }));
    expect(within(screen.getByRole('region', { name: '候选列表' })).getByRole('button')).toBeTruthy();
  });

  it('explains a blocked in-progress candidate without sending a write', () => {
    const { rerender, props } = renderCandidates([pending]);
    const write = vi.spyOn(props.client, 'write');
    rerender(<Candidates {...props} snapshot={{ ...props.snapshot, tasks: [{ ...localTask, id: pending.task_id, hold_reason: 'human_decision' }] }} />);
    expect(screen.getByText(/任务受阻：等待人工决策/)).toBeTruthy();
    expect(screen.getByRole('link', { name: localTask.title }).getAttribute('href')).toBe('#tasks?id=task-1');
    expect(write).not.toHaveBeenCalled();
  });

  it('restores route filters and preserves a linked historical detail', () => {
    window.history.replaceState({}, '', '#candidates?id=old&filter=history');
    try {
      renderCandidates([pending, { ...pending, id: 'old', state: 'landed' }]);
      const list = within(screen.getByRole('region', { name: '候选列表' }));
      expect(list.getByRole('button').getAttribute('aria-pressed')).toBe('true');
      fireEvent.click(screen.getByRole('button', { name: '进行中' }));
      expect(list.getByRole('button').getAttribute('aria-pressed')).toBe('false');
      expect(screen.getByRole('link', { name: '审查永久链接' }).getAttribute('href')).toBe('#candidates?id=old');
      expect(screen.getByText(/此详情不在当前筛选范围/)).toBeTruthy();
      window.history.replaceState({}, '', '#candidates?id=old&filter=history');
      fireEvent(window, new PopStateEvent('popstate'));
      expect(screen.queryByText(/此详情不在当前筛选范围/)).toBeNull();
      expect(list.getByRole('button').getAttribute('aria-pressed')).toBe('true');
    } finally {
      window.history.replaceState({}, '', '/');
    }
  });

  it('keeps terminal and unknown states out of progress and preserves all, history, state and search filters', () => {
    renderCandidates([...activeStates, ...historyStates, 'future_state'].map((state) => ({ ...pending, id: state, state })));
    const list = within(screen.getByRole('region', { name: '候选列表' }));
    expect(list.getAllByRole('button')).toHaveLength(activeStates.length);
    fireEvent.click(screen.getByRole('button', { name: '历史' }));
    expect(list.getAllByRole('button')).toHaveLength(historyStates.length);
    fireEvent.click(screen.getByRole('button', { name: '全部' }));
    expect(list.getAllByRole('button')).toHaveLength(activeStates.length + historyStates.length + 1);
    fireEvent.change(screen.getByLabelText('审查筛选'), { target: { value: 'prechecking' } });
    expect(list.getAllByRole('button')).toHaveLength(1);
    fireEvent.change(screen.getByLabelText('搜索审查'), { target: { value: 'not-found' } });
    expect(list.queryByRole('button')).toBeNull();
    fireEvent.change(screen.getByLabelText('搜索审查'), { target: { value: '' } });
    fireEvent.click(screen.getByRole('button', { name: '进行中' }));
    expect(list.getAllByRole('button')).toHaveLength(activeStates.length);
  });
});
