import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import type { AutomationSettings, LeaderConfiguration } from '../../api.generated';
import { ApiClient } from '../../api/client';
import { centerSnapshot } from '../../test/connectionInfo';
import { readyInitialization } from '../../test/initialization';
import { ProjectInitialization } from '../ProjectInitialization';

const flash = '["provider","flash"]';
const catalog: LeaderConfiguration = {
  version: 7, queried_at: '', selection: { tool: 'codex', model: 'gpt-6.1-sol' },
  capabilities: ['codex', 'dsh', 'uninstalled'].map((tool) => ({
    tool, installed: tool !== 'uninstalled', available: true, structured: true,
    cancel: true, review: true, read_only: true, resume: false, version: '1', protocol: 'fixture', detail: 'fixture protocol',
    model_discovery: 'reported', models: tool === 'codex' ? [
      { id: 'gpt-6.1-sol', name: 'Sol', reasoning_efforts: ['high', 'ultra'] },
      { id: 'another-reported-model', name: 'Another model', reasoning_efforts: ['low'] },
    ] : [{ id: flash, name: 'Flash', reasoning_efforts: ['high', 'max'] }],
  })),
};

function setup(configuration = catalog) {
  const client = new ApiClient('owner');
  const read = vi.spyOn(client, 'read').mockResolvedValue(configuration);
  const write = vi.spyOn(client, 'write').mockResolvedValue(readyInitialization);
  render(<ProjectInitialization snapshot={{ ...centerSnapshot, initialization: null,
    project: { ...centerSnapshot.project!, paused: true },
  }} client={client} refresh={vi.fn()} />);
  fireEvent.click(screen.getByText('Leader 工具与模型'));
  return { read, write };
}
const choose = (label: string, value: string) => fireEvent.change(screen.getByLabelText(label), { target: { value } });
const analyze = () => screen.getByRole('button', { name: '打开项目并请求只读初始化' });
const save = () => screen.getByRole('button', { name: '保存 Leader 供交互层使用' });
async function selectDsh() {
  await screen.findByRole('option', { name: 'dsh' });
  choose('Leader 工具', 'dsh');
}

it('offers installed harnesses and all reported models, with model-specific efforts only', async () => {
  setup();
  await screen.findByRole('option', { name: 'Another model' });
  expect(screen.queryByRole('option', { name: 'uninstalled' })).toBeNull();
  expect(screen.getByRole('option', { name: 'ultra' })).toBeTruthy();
  choose('Leader 模型', 'another-reported-model');
  expect(screen.queryByRole('option', { name: 'ultra' })).toBeNull();
  expect(screen.getByRole('option', { name: 'low' })).toBeTruthy();
  await selectDsh();
  expect(screen.getByLabelText('Leader 模型')).toHaveProperty('value', flash);
  expect(screen.queryByRole('option', { name: 'ultra' })).toBeNull();
});

it('distinguishes duplicate model names by reported provider without changing opaque IDs', async () => {
  const other = '["account","flash"]';
  const { write } = setup({ ...catalog, capabilities: catalog.capabilities!.map((cap) => cap.tool !== 'dsh' ? cap : {
    ...cap, models: [...cap.models!, { id: other, name: 'Flash', reasoning_efforts: [] }],
  }) });
  await selectDsh();
  expect(screen.getByRole('option', { name: 'Flash · provider' })).toBeTruthy();
  expect(screen.getByRole('option', { name: 'Flash · account' })).toBeTruthy();
  choose('Leader 模型', other);
  fireEvent.click(analyze());
  await waitFor(() => expect(write).toHaveBeenCalledWith('/initialization/analyze', {
    expected_version: 0, kind: 'existing', leader_version: 7, leader: { tool: 'dsh', model: other },
  }));
});

it('pins an explicit choice and configuration version in the initialization request', async () => {
  const { write } = setup();
  await selectDsh();
  choose('Leader 思考强度', 'max');
  fireEvent.click(analyze());
  await waitFor(() => expect(write).toHaveBeenCalledWith('/initialization/analyze', {
    expected_version: 0, kind: 'existing', leader_version: 7,
    leader: { tool: 'dsh', model: flash, reasoning_effort: 'max' },
  }));
});

it('can save the Leader for unified MCP without opening a project or enabling dispatch', async () => {
  const { write } = setup();
  write.mockResolvedValue({ version: 8, leader_tool: 'dsh', leader_model: flash } as AutomationSettings);
  await selectDsh();
  fireEvent.click(save());
  await screen.findByText('Leader 已保存；项目暂停和自动化设置未改变。');
  expect(write).toHaveBeenCalledTimes(1);
  expect(write).toHaveBeenCalledWith('/leader/configuration', {
    expected_version: 7, selection: { tool: 'dsh', model: flash },
  });
  expect(save()).toHaveProperty('disabled', true);
});

it('keeps a selection whose model disappears on refresh and blocks a new execution', async () => {
  const { read, write } = setup();
  await selectDsh();
  read.mockResolvedValue({ ...catalog, version: 8, capabilities: catalog.capabilities!.filter((cap) => cap.tool !== 'dsh') });
  fireEvent.click(screen.getByRole('button', { name: '刷新 Leader 能力' }));
  await screen.findByText('所选工具或模型当前不可用。刷新能力或另选工具。');
  expect(screen.getByLabelText('Leader 工具')).toHaveProperty('value', 'dsh');
  expect(analyze()).toHaveProperty('disabled', true);
  expect(write).not.toHaveBeenCalled();
});

it('preserves draft options after a rejected save and retains the exact original version', async () => {
  const { write } = setup();
  write.mockRejectedValue(new Error('版本已变化'));
  await selectDsh();
  choose('Leader 思考强度', 'high');
  fireEvent.click(save());
  await screen.findByText('版本已变化');
  expect(screen.getByLabelText('Leader 思考强度')).toHaveProperty('value', 'high');
  fireEvent.click(save());
  await waitFor(() => expect(write).toHaveBeenCalledTimes(2));
  expect(write.mock.calls[1]).toEqual(write.mock.calls[0]);
});

it('does not replace a newer draft with a late save receipt', async () => {
  const { write } = setup();
  let accept!: (value: AutomationSettings) => void;
  write.mockImplementationOnce(() => new Promise((resolve) => { accept = resolve; }));
  await selectDsh();
  fireEvent.click(save());
  choose('Leader 思考强度', 'max');
  accept({ version: 8, leader_tool: 'dsh', leader_model: flash } as AutomationSettings);
  await screen.findByText('Leader 已保存；项目暂停和自动化设置未改变。');
  expect(screen.getByLabelText('Leader 思考强度')).toHaveProperty('value', 'max');
  fireEvent.click(save());
  await waitFor(() => expect(write).toHaveBeenLastCalledWith('/leader/configuration', {
    expected_version: 8, selection: { tool: 'dsh', model: flash, reasoning_effort: 'max' },
  }));
});
