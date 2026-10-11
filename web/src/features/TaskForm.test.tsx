import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import type { Capability, Node } from '../api.generated';
import { ApiClient } from '../api/client';
import { TaskForm } from './TaskForm';

function node(id: string, capabilities: Capability[], revoked = false): Node {
  return { id, version: 1, name: id, platform: 'test', capacity: 1, capabilities, revoked, last_seen: '' };
}
function capability(tool: string, overrides: Partial<Capability> = {}): Capability {
  return { tool, version: 'fixture', available: true, structured: true, resume: false, cancel: true, review: true, read_only: false, protocol: 'acp', detail: '', ...overrides };
}
function form(nodes: Node[], client = new ApiClient('token')) {
  return render(<TaskForm nodes={nodes} requirements={[]} tasks={[]} client={client} refresh={() => undefined} onCreated={() => undefined} onCancel={() => undefined} />);
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
