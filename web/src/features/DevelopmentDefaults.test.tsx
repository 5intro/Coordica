import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import { ApiClient } from '../api/client';
import { centerSnapshot } from '../test/connectionInfo';
import { nodeSnapshot } from '../test/nodeSnapshot';
import { Automation } from './Automation';
import { developmentCapabilities } from './DevelopmentDefaults';

const node = { ...nodeSnapshot.node, capabilities: [{ ...nodeSnapshot.node.capabilities![0],
  tool: 'studio-agent', available: true, structured: true, cancel: true, review: false,
  model_discovery: 'reported', models: [{ id: 'provider/flash', name: 'Flash', reasoning_efforts: [] }],
}] };

it('selects an arbitrary reported development tool and model without enabling automation', async () => {
  const client = new ApiClient('owner');
  const write = vi.spyOn(client, 'write').mockResolvedValue({});
  render(<Automation settings={centerSnapshot.automation} nodes={[node]} client={client} refresh={vi.fn()} />);
  const tools = screen.getByLabelText('默认开发工具');
  expect(within(tools).getAllByRole('option', { hidden: true }).map((option) => option.getAttribute('value')))
    .toEqual(['codex', 'studio-agent']);
  fireEvent.change(tools, { target: { value: 'studio-agent' } });
  expect(screen.getByLabelText('默认开发模型')).toHaveProperty('value', 'provider/flash');
  expect(screen.getByText(/未保存：修改/)).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: '保存系统自动化' }));
  await waitFor(() => expect(write).toHaveBeenCalledWith('/system/automation', {
    expected_version: centerSnapshot.automation.version,
    settings: { ...centerSnapshot.automation, development_tool: 'studio-agent', development_model: 'provider/flash' },
  }));
});

it('preserves a lost selection and refuses to substitute another model on save', () => {
  const client = new ApiClient('owner');
  const write = vi.spyOn(client, 'write');
  const view = render(<Automation settings={centerSnapshot.automation} nodes={[node]} client={client} refresh={vi.fn()} />);
  fireEvent.change(screen.getByLabelText('默认开发工具'), { target: { value: 'studio-agent' } });
  view.rerender(<Automation settings={centerSnapshot.automation} nodes={[]} client={client} refresh={vi.fn()} />);
  expect(screen.getByLabelText('默认开发模型')).toHaveProperty('value', 'provider/flash');
  fireEvent.click(screen.getByRole('button', { name: '保存系统自动化' }));
  expect(screen.getByRole('alert').textContent).toContain('已不在主机能力报告');
  expect(write).not.toHaveBeenCalled();
});

it('merges reported models without adding unavailable, revoked or guessed harnesses', () => {
  const other = { ...node, id: 'second', capabilities: [{ ...node.capabilities[0],
    models: [{ id: 'provider/other', name: 'Other', reasoning_efforts: [] }],
  }] };
  const unavailable = { ...node, id: 'missing', capabilities: [{ ...node.capabilities[0], tool: 'missing', available: false }] };
  const revoked = { ...node, id: 'revoked', revoked: true, capabilities: [{ ...node.capabilities[0], tool: 'revoked' }] };
  const result = developmentCapabilities([node, other, unavailable, revoked]);
  expect(result.map((cap) => cap.tool)).toEqual(['studio-agent']);
  expect(result[0].models?.map((model) => model.id)).toEqual(['provider/flash', 'provider/other']);
});
