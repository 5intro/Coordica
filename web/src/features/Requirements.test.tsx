import { fireEvent, render, screen, within } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import type { Requirement } from '../api.generated';
import { ApiClient } from '../api/client';
import { Requirements } from './Requirements';

it('renders distinct chat original/summary plus host provenance and filters/searches both', () => {
  const chat: Requirement = { id: 'req_chat', version: 1, title: 'Chat requirement', body: 'Old body', original_text: 'Original user statement', assistant_summary: 'Distinct assistant summary', source_node_id: 'node_host', source: { client_id: 'client_host', session_id: 'session_42', message_id: 'message_99' }, created_at: '' };
  const old: Requirement = { id: 'req_old', version: 1, title: 'Old center record', body: 'Legacy original body', created_at: '' };
  render(<Requirements requirements={[chat, old]} client={new ApiClient('owner')} refresh={vi.fn()} />);
  expect(screen.getByText('Original user statement')).toBeTruthy();
  expect(screen.getByText('Distinct assistant summary')).toBeTruthy();
  expect(screen.getAllByText(/client_host \/ session_42 \/ message_99/)).toBeTruthy();
  expect(screen.getByText(/来源：节点来源/)).toBeTruthy();
  const table = screen.getByRole('table');
  for (const value of ['Original user statement', 'Distinct assistant summary', 'session_42', 'message_99', 'node_host']) {
    fireEvent.change(screen.getByRole('searchbox'), { target: { value } });
    expect(within(table).getByText('Chat requirement')).toBeTruthy();
    expect(within(table).queryByText('Old center record')).toBeNull();
  }
  fireEvent.change(screen.getByRole('searchbox'), { target: { value: '' } });
  fireEvent.change(screen.getByLabelText('需求来源'), { target: { value: 'center' } });
  expect(within(table).queryByText('Chat requirement')).toBeNull();
  fireEvent.click(within(table).getByRole('button', { name: 'Old center record' }));
  expect(screen.getByText('Legacy original body')).toBeTruthy();
});

it('identifies a center-host chat requirement without a source node as chat provenance', () => {
  const requirement: Requirement = { id: 'req_center_chat', version: 1, title: 'Center chat input', body: 'Original from chat', assistant_summary: 'Assistant interpretation', source: { client_id: 'client_center', session_id: 'session_center', message_id: 'message_center' }, created_at: '' };
  render(<Requirements requirements={[requirement]} client={new ApiClient('owner')} refresh={vi.fn()} />);
  expect(screen.getAllByText(/client_center \/ session_center \/ message_center/)).toBeTruthy();
  expect(screen.getByText('Assistant interpretation')).toBeTruthy();
  fireEvent.change(screen.getByLabelText('需求来源'), { target: { value: 'center' } });
  expect(screen.queryByRole('table')).toBeNull();
  fireEvent.change(screen.getByLabelText('需求来源'), { target: { value: 'chat' } });
  expect(within(screen.getByRole('table')).getByRole('button', { name: 'Center chat input' })).toBeTruthy();
});
