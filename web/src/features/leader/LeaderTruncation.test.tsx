import { act, render, screen } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import type { LeaderActivityPage } from '../../api.generated';
import { ApiClient } from '../../api/client';
import { LeaderActivityPanel } from './LeaderActivityPanel';

it.each(['client-pruning', 'retention-gap'])('retains the %s warning through subsequent incremental pages', (cause) => {
  const client = new ApiClient('owner');
  let receive!: (page: LeaderActivityPage) => void;
  vi.spyOn(client, 'followLeader').mockImplementation(async (_after, _signal, next) => { receive = next; });
  render(<LeaderActivityPanel client={client} />);
  const ref = { kind: 'planning', id: 'run', generation: 1, input_version: 1 };
  const page = (offset: number, count: number, truncated = false): LeaderActivityPage => ({
    version: 1, cursor: offset + count, oldest: 1, has_more: false, truncated, queried_at: '',
    executions: [{ ref, version: 1, purpose: '', source_id: '', state: 'running', tree_stopped: false, settled: false }],
    events: Array.from({ length: count }, (_, index) => ({ sequence: offset + index + 1, execution: ref,
      key: String(offset + index), kind: 'message', text: `activity-${offset + index}`, time: '' })),
  });
  if (cause === 'retention-gap') act(() => receive(page(0, 1, true)));
  else {
    act(() => receive(page(0, 100)));
    act(() => receive(page(100, 100)));
    expect(screen.queryByText('仅保留最近的活动，早期记录已省略。')).toBeNull();
    act(() => receive(page(200, 1)));
  }
  expect(screen.getByText('仅保留最近的活动，早期记录已省略。')).toBeTruthy();
  act(() => receive(page(201, 1)));
  expect(screen.getByText('仅保留最近的活动，早期记录已省略。')).toBeTruthy();
});
