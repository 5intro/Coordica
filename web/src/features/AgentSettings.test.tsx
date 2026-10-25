import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import type { Capability } from '../api.generated';
import { ApiClient } from '../api/client';
import { nodeSnapshot } from '../test/nodeSnapshot';
import { AgentSettings } from './AgentSettings';
const capability: Capability = { ...nodeSnapshot.node.capabilities![0], tool: 'codex', protocol: 'codex-jsonl', model_discovery: 'reported', models: [{ id: 'gpt-6.1-sol', name: 'GPT-6.1 Sol', reasoning_efforts: ['high', 'ultra'], default_reasoning_effort: 'high' }, { id: 'gpt-6-astra', name: 'Astra', reasoning_efforts: ['ultra'] }] };
const other = { tool: 'other', enabled: false, implement: false, review: true, model: 'other-model' };
const node = { ...nodeSnapshot.node, scheduling_configured: true, tools: [other, { tool: 'codex', enabled: true, implement: true, review: false }], capabilities: [capability] };
it.each(['codex', 'claude', 'custom-acp'])('does not present %s role permissions as host isolation', (tool) => {
 const client = new ApiClient('owner'), write = vi.spyOn(client, 'write');
 render(<AgentSettings node={node} capability={{ ...capability, tool, read_only: true }} tool={tool} client={client} refresh={vi.fn()}/>);
 expect(screen.getByText(/范围检查限制代码集成，不隔离电脑文件/)).toBeTruthy();
 expect(write).not.toHaveBeenCalled();
});
it('saves real per-tool parameters with the original host version and preserves other tool permissions on retries', async () => {
 const client = new ApiClient('owner'), refresh = vi.fn();
 const write = vi.spyOn(client, 'write').mockRejectedValueOnce(new Error('中心拒绝')).mockResolvedValueOnce({ ...node, version: 2 });
 render(<AgentSettings node={node} capability={capability} tool="codex" client={client} refresh={refresh}/>);
 const models = screen.getByLabelText('执行模型');
 expect(within(models).getAllByRole('option').every((option) => !(option as HTMLOptionElement).disabled)).toBe(true);
 fireEvent.change(models, { target: { value: 'gpt-6.1-sol' } });
 expect(within(screen.getByLabelText('思考强度')).queryByRole('option', { name: 'max' })).toBeNull();
 fireEvent.change(screen.getByLabelText('思考强度'), { target: { value: 'ultra' } });
 fireEvent.change(models, { target: { value: 'gpt-6-astra' } });
 expect(screen.getByLabelText('思考强度')).toHaveProperty('value', '');
 expect(within(screen.getByLabelText('思考强度')).queryByRole('option', { name: 'high' })).toBeNull();
 fireEvent.change(screen.getByLabelText('思考强度'), { target: { value: 'ultra' } });
 fireEvent.click(screen.getByRole('button', { name: '保存 Agent 配置' }));
 await screen.findByText('中心拒绝');
 expect(screen.getByLabelText('思考强度')).toHaveProperty('value', 'ultra');
 fireEvent.click(screen.getByRole('button', { name: '保存 Agent 配置' }));
 await screen.findByText('已保存，仅用于后续新运行。');
 expect(write).toHaveBeenLastCalledWith('/nodes/node_local/configure', { expected_version: 1, name: node.name, capacity: node.capacity, draining: false, disabled_new_work: false, tools: [other, { tool: 'codex', enabled: true, implement: true, review: false, model: 'gpt-6-astra', reasoning_effort: 'ultra' }] });
});
it('retains drafts across newer host settings and refuses stale writes until explicitly reloaded', async () => {
 const client = new ApiClient('owner'), write = vi.spyOn(client, 'write'), refresh = vi.fn();
 const view = render(<AgentSettings node={node} capability={capability} tool="codex" client={client} refresh={refresh}/>);
 fireEvent.change(screen.getByLabelText('执行模型'), { target: { value: 'gpt-6.1-sol' } });
 fireEvent.change(screen.getByLabelText('思考强度'), { target: { value: 'ultra' } });
 view.rerender(<AgentSettings node={{ ...node, version: 2 }} capability={capability} tool="codex" client={client} refresh={refresh}/>);
 fireEvent.click(screen.getByRole('button', { name: '保存 Agent 配置' }));
 expect(screen.getByRole('alert').textContent).toContain('重新载入');
 expect(screen.getByLabelText('思考强度')).toHaveProperty('value', 'ultra');
 expect(write).not.toHaveBeenCalled();
 fireEvent.click(screen.getByRole('button', { name: '重新载入' }));
 expect(screen.getByLabelText('执行模型')).toHaveProperty('value', '');
});
it('shows no guessed menus and discovers new effort values after a tool update', async () => {
 const client = new ApiClient('owner'), write = vi.spyOn(client, 'write');
 const old = { ...capability, tool: 'claude', protocol: 'claude-stream-json', models: [{ id: 'native-model', name: 'Native model', reasoning_efforts: ['high', 'max'] }] };
 const view = render(<AgentSettings node={node} capability={old} tool="claude" client={client} refresh={vi.fn()}/>);
 fireEvent.change(screen.getByLabelText('执行模型'), { target: { value: 'native-model' } });
 expect(within(screen.getByLabelText('思考强度')).queryByRole('option', { name: 'ultra' })).toBeNull();
 view.rerender(<AgentSettings node={node} capability={{ ...old, models: [{ ...old.models[0], reasoning_efforts: ['high', 'max', 'ultra'] }] }} tool="claude" client={client} refresh={vi.fn()}/>);
 expect(within(screen.getByLabelText('思考强度')).getByRole('option', { name: 'ultra' })).toBeTruthy();
 expect(write).not.toHaveBeenCalled();
 view.unmount();
 render(<AgentSettings node={node} capability={{ ...capability, models: undefined, model_discovery: 'unavailable' }} tool="codex" client={client} refresh={vi.fn()}/>);
 expect(screen.queryByRole('combobox', { name: /模型|强度/ })).toBeNull();
 expect(screen.getByText('模型目录暂时无法读取。')).toBeTruthy();
 await waitFor(() => expect(write).not.toHaveBeenCalled());
});

it.each(['允许调度', '开发任务', '审查任务'])('can reduce %s while keeping obsolete execution parameters', async (label) => {
 const client = new ApiClient('owner');const write = vi.spyOn(client,'write').mockResolvedValue({});
 const saved = {...node,tools:[{tool:'codex',enabled:true,implement:true,review:true,model:'removed-model',reasoning_effort:'removed-effort'}]};
 render(<AgentSettings node={saved} capability={capability} tool="codex" client={client} refresh={vi.fn()}/>);
 fireEvent.click(screen.getByLabelText(label));fireEvent.click(screen.getByRole('button',{name:'保存 Agent 配置'}));
 await waitFor(() => expect(write).toHaveBeenCalledTimes(1));
 expect(write.mock.calls[0][1]).toMatchObject({tools:[{model:'removed-model',reasoning_effort:'removed-effort',[{'允许调度':'enabled','开发任务':'implement','审查任务':'review'}[label]!]:false}]});
});
it('still rejects increasing permissions with obsolete model parameters', () => {
 const client=new ApiClient('owner');const write=vi.spyOn(client,'write');
 const saved={...node,tools:[{tool:'codex',enabled:false,implement:true,review:true,model:'removed-model',reasoning_effort:'removed-effort'}]};
 render(<AgentSettings node={saved} capability={capability} tool="codex" client={client} refresh={vi.fn()}/>);
 fireEvent.click(screen.getByLabelText('允许调度'));fireEvent.click(screen.getByRole('button',{name:'保存 Agent 配置'}));
 expect(screen.getByRole('alert').textContent).toContain('不在最新能力报告');expect(write).not.toHaveBeenCalled();
});
