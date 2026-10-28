import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import type { Capability, Node, Snapshot } from '../api.generated';
import { ApiClient } from '../api/client';
import { emptyPhaseB } from '../test/phaseB';
import { TaskForm } from './TaskForm';
import { emptyGoal } from './Planning';
import { Tasks } from './Tasks';
import { centerSnapshot } from '../test/connectionInfo';

function node(id: string, capabilities: Capability[], revoked = false): Node {
  return { draining: false, disabled_new_work: false, scheduling_configured: false, tools: [], id, version: 1, name: id, platform: 'test', capacity: 1, capabilities, revoked, last_seen: '' };
}
function capability(tool: string, overrides: Partial<Capability> = {}): Capability {
  return { tool, version: 'fixture', available: true, structured: true, resume: false, cancel: true, review: true, read_only: false, protocol: 'acp', detail: '', ...overrides };
}
function form(nodes: Node[], client = new ApiClient('token'), snapshot: Pick<Snapshot, 'planning_document' | 'automation'> = emptyPhaseB) {
  return render(<TaskForm snapshot={snapshot} nodes={nodes} requirements={[]} tasks={[]} client={client} refresh={() => undefined} onCreated={() => undefined} onCancel={() => undefined} />);
}

describe('discovered task tools', () => {
  it('offers custom identities once and excludes revoked or insufficient capabilities', () => {
    const { container } = form([
      node('one', [capability('custom-acp'), capability('uncancelable', { cancel: false }), capability('absent', { available: false })]),
      node('two', [capability('custom-acp'), capability('plain-text', { structured: false })]),
      node('revoked', [capability('revoked-tool')], true),
    ]);
    expect([...container.querySelectorAll('datalist option')].map(option => option.getAttribute('value'))).toEqual(['custom-acp']);
  });

  it('saves a draft for a tool before its node is online', async () => {
    const client = new ApiClient('token');
    const write = vi.spyOn(client, 'write').mockResolvedValue({ id: 'task-custom' });
    const { container } = form([], client);
    fireEvent.change(screen.getByLabelText('任务标题'), { target: { value: 'Custom task' } });
    fireEvent.change(screen.getByLabelText('目标与非目标'), { target: { value: 'Implement greeting' } });
    fireEvent.change(screen.getByLabelText('可观察验收条件'), { target: { value: 'Independent verifier passes' } });
    fireEvent.change(container.querySelector('[name="tool"]')!, { target: { value: 'custom-acp' } });
    fireEvent.change(container.querySelector('[name="paths"]')!, { target: { value: 'src/**' } });
    fireEvent.click(screen.getByRole('button', { name: '保存草稿' }));
    await waitFor(() => expect(write).toHaveBeenCalledWith('/tasks', expect.objectContaining({ tool: 'custom-acp', allowed_paths: ['src/**'] })));
  });
});

const mixedNodes = [node('mixed', ['claude', 'codex', 'dsh'].map(tool => capability(tool)))];
const goalSnapshot = { ...emptyPhaseB, planning_document: emptyGoal };
function fillDraft(container: HTMLElement) {
  fireEvent.change(screen.getByLabelText('任务标题'), { target: { value: 'Goal task' } });
  fireEvent.change(screen.getByLabelText('目标与非目标'), { target: { value: 'Implement greeting' } });
  fireEvent.change(screen.getByLabelText('可观察验收条件'), { target: { value: 'Verifier passes' } });
  fireEvent.change(container.querySelector('[name="paths"]')!, { target: { value: 'web/src/**' } });
}

describe('goal-bound development tool', () => {
  it('uses center settings for mixed capabilities and enforces them on submission', async () => {
    const client = new ApiClient('fixture');
    const write = vi.spyOn(client, 'write').mockResolvedValue({ id: 'goal-task' });
    const { container } = form(mixedNodes, client, goalSnapshot);
    const input = screen.getByLabelText(/^实现工具/) as HTMLInputElement;
    expect(input.value).toBe('codex');
    expect(input.readOnly).toBe(true);
    expect([...container.querySelectorAll('datalist option')].map(option => option.getAttribute('value'))).toEqual(['codex']);
    fillDraft(container);
    // A modified form value cannot substitute an incompatible development tool.
    fireEvent.change(input, { target: { value: 'claude' } });
    fireEvent.click(screen.getByRole('button', { name: '保存草稿' }));
    await waitFor(() => expect(write).toHaveBeenCalledWith('/tasks', expect.objectContaining({ tool: 'codex' })));
  });

  it('preserves alphabetical dynamic discovery without a goal', () => {
    const { container } = form(mixedNodes);
    expect((screen.getByLabelText(/^实现工具/) as HTMLInputElement).value).toBe('claude');
    expect((screen.getByLabelText(/^实现工具/) as HTMLInputElement).readOnly).toBe(false);
    expect([...container.querySelectorAll('datalist option')].map(option => option.getAttribute('value'))).toEqual(['claude', 'codex', 'dsh']);
  });

  it.each([
    { nodes: [] },
    { nodes: [node('other', [capability('claude'), capability('dsh')])] },
    { nodes: [node('revoked', [capability('codex')], true)] },
    { nodes: [node('unavailable', [capability('codex', { available: false })])] },
    { nodes: [node('plain', [capability('codex', { structured: false })])] },
    { nodes: [node('uncancelable', [capability('codex', { cancel: false })])] },
  ])('keeps the safe tool while waiting for compatible reports (%#)', async ({ nodes }) => {
    const client = new ApiClient('fixture');
    const write = vi.spyOn(client, 'write').mockResolvedValue({ id: 'waiting-task' });
    const { container } = form(nodes, client, goalSnapshot);
    expect((screen.getByLabelText(/^实现工具/) as HTMLInputElement).value).toBe('codex');
    expect(screen.getByText(/等待未撤销节点报告该工具可用/)).toBeTruthy();
    fillDraft(container);
    fireEvent.click(screen.getByRole('button', { name: '保存草稿' }));
    await waitFor(() => expect(write).toHaveBeenCalledWith('/tasks', expect.objectContaining({ tool: 'codex' })));
  });

  it('follows updated center settings while the form remains open', () => {
    const props = { nodes: mixedNodes, requirements: [], tasks: [], client: new ApiClient('fixture'), refresh: () => undefined, onCreated: () => undefined, onCancel: () => undefined };
    const view = render(<TaskForm {...props} snapshot={emptyPhaseB} />);
    expect((screen.getByLabelText(/^实现工具/) as HTMLInputElement).value).toBe('claude');
    view.rerender(<TaskForm {...props} snapshot={goalSnapshot} />);
    expect((screen.getByLabelText(/^实现工具/) as HTMLInputElement).value).toBe('codex');
    view.rerender(<TaskForm {...props} snapshot={{ ...goalSnapshot, automation: { ...goalSnapshot.automation, development_tool: 'center-custom' } }} />);
    expect((screen.getByLabelText(/^实现工具/) as HTMLInputElement).value).toBe('center-custom');
    view.rerender(<TaskForm {...props} snapshot={emptyPhaseB} />);
    expect((screen.getByLabelText(/^实现工具/) as HTMLInputElement).value).toBe('claude');
  });

  it('does not invent a fallback when the center tool is empty', () => {
    form(mixedNodes, new ApiClient('fixture'), { ...goalSnapshot, automation: { ...goalSnapshot.automation, development_tool: '' } });
    expect((screen.getByLabelText(/^实现工具/) as HTMLInputElement).value).toBe('');
    expect((screen.getByRole('button', { name: '保存草稿' }) as HTMLButtonElement).disabled).toBe(true);
  });

  it('receives the actual goal and automation from the Tasks caller', () => {
    render(<Tasks snapshot={{ ...centerSnapshot, ...goalSnapshot, nodes: mixedNodes }} client={new ApiClient('fixture')} refresh={() => undefined} initialId="new" />);
    expect((screen.getByLabelText(/^实现工具/) as HTMLInputElement).value).toBe('codex');
    expect((screen.getByLabelText(/^实现工具/) as HTMLInputElement).readOnly).toBe(true);
  });
});

it('creates a task without installing hidden default time allowances', async () => {
  const client = new ApiClient('fixture');
  const write = vi.spyOn(client, 'write').mockResolvedValue({ id: 'unbounded-task' });
  const { container } = form(mixedNodes, client, goalSnapshot);
  fillDraft(container);
  fireEvent.click(screen.getByRole('button', { name: '保存草稿' }));
  await waitFor(() => expect(write).toHaveBeenCalledWith('/tasks', expect.objectContaining({ budget_seconds: 0, max_run_seconds: 0 })));
});
