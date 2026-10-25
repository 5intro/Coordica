import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import type { Initialization } from '../api.generated';
import { ApiClient } from '../api/client';
import { centerSnapshot } from '../test/connectionInfo';
import { readyInitialization } from '../test/initialization';
import { ProjectInitialization } from './ProjectInitialization';

const draft = (kind: string, version = 3): Initialization => ({
  ...readyInitialization, kind, version, status: 'draft', result: null,
});
const selection = () => screen.getByLabelText(/项目输入类型/);
const choose = (value: string) => fireEvent.change(selection(), { target: { value } });
const analyze = () => fireEvent.click(screen.getByRole('button', { name: '打开项目并请求只读初始化' }));

function setup(current: Initialization | null = null) {
  const client = new ApiClient('owner');
  const write = vi.spyOn(client, 'write').mockResolvedValue(readyInitialization);
  const show = (value: Initialization | null) => (
    <ProjectInitialization snapshot={{ ...centerSnapshot,
      project: { ...centerSnapshot.project!, paused: true }, initialization: value,
    }} client={client} refresh={vi.fn()} />
  );
  const view = render(show(current));
  return { write, update: (value: Initialization | null) => view.rerender(show(value)) };
}

it('follows an externally classified new project after an analysis fails or is cancelled', async () => {
  const view = setup();
  expect(selection()).toHaveProperty('value', 'existing');
  view.update({ ...draft('new', 1), status: 'analyzing' });
  expect(selection()).toHaveProperty('value', 'new');
  view.update(draft('new', 2));
  analyze();
  await waitFor(() => expect(view.write).toHaveBeenCalledWith('/initialization/analyze', {
    expected_version: 2, kind: 'new',
  }));
});

it('preserves an explicit unsent Owner choice across external kind changes', async () => {
  const view = setup();
  choose('existing');
  view.update(draft('new'));
  expect(selection()).toHaveProperty('value', 'existing');
  analyze();
  await waitFor(() => expect(view.write).toHaveBeenCalledWith('/initialization/analyze', {
    expected_version: 3, kind: 'existing',
  }));
});

it('returns to following the server only after that exact local choice is accepted', async () => {
  const view = setup(draft('existing'));
  choose('new');
  analyze();
  await screen.findByText('只读分析请求已登记。未授权任何开发任务。');
  view.update(draft('existing', 4));
  expect(selection()).toHaveProperty('value', 'existing');
});

it('keeps a newer choice even when it has the same value as the choice in a late receipt', async () => {
  const view = setup(draft('existing'));
  let accept!: (value: Initialization) => void;
  view.write.mockImplementationOnce(() => new Promise((resolve) => { accept = resolve; }));
  choose('new');
  analyze();
  choose('existing');
  choose('new');
  accept(readyInitialization);
  await screen.findByText('只读分析请求已登记。未授权任何开发任务。');
  view.update(draft('existing', 4));
  expect(selection()).toHaveProperty('value', 'new');
});

it('retains a rejected local choice instead of replacing it with a newer external default', async () => {
  const view = setup(draft('existing'));
  view.write.mockRejectedValueOnce(new Error('stale version'));
  choose('new');
  analyze();
  await screen.findByText('stale version');
  view.update(draft('existing', 4));
  expect(selection()).toHaveProperty('value', 'new');
});
