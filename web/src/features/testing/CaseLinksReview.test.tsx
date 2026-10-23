import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import type { AcceptancePlan, TestCase } from '../../api.generated';
import { ApiClient } from '../../api/client';
import { centerSnapshot } from '../../test/connectionInfo';
import { Testing } from './TestingWorkspace';

afterEach(() => window.history.replaceState({}, '', '#'));

it('reloads the current plan on return and replaces stale references instead of presenting them as linked', async () => {
  window.history.replaceState({}, '', '#testing');
  const requirement = { id: 'req', version: 1, content_revision: 1, title: 'Payment', body: 'Once', state: 'active', created_at: '' };
  let plan: AcceptancePlan = { requirement_id: 'req', revision: 1, version: 1,
    criteria: [{ id: 'once', title: 'Payment once', method: 'automated' }] };
  const item: TestCase = { id: 'pay', version: 1, name: 'Payment retry', purpose: 'Avoid doubles', categories: ['regression'],
    steps: ['Retry'], assertions: ['Once'], enabled: true, implemented: false, suite_id: '', check_name: '', external_id: '', report_format: '',
    coverage: [{ requirement_id: 'req', revision: 1, plan_version: 1, criterion_id: 'once' }] };
  const client = new ApiClient('owner');
  vi.spyOn(client, 'read').mockImplementation(async (path) => path.startsWith('/tests/acceptance/plan') ? plan : { items: [item], next: '' });
  const write = vi.spyOn(client, 'write').mockImplementation(async (path, body) => {
    if (path === '/tests/acceptance') {
      plan = { ...plan, version: 2, criteria: [{ id: 'once', title: 'Updated payment once', method: 'automated' }] };
      return plan;
    }
    return { ...(body as { case: TestCase }).case, version: 2 };
  });
  render(<Testing snapshot={{ ...centerSnapshot, requirements: [requirement] }} client={client} refresh={vi.fn()} />);
  fireEvent.click(await screen.findByRole('button', { name: /Payment retry/ }));
  fireEvent.change(screen.getByLabelText('添加关联需求'), { target: { value: 'req' } });
  expect(await screen.findByRole('checkbox', { name: 'Payment once' })).toHaveProperty('checked', true);
  fireEvent.click(screen.getByRole('button', { name: '需求验收与补测' }));
  fireEvent.change(screen.getByLabelText('关联需求'), { target: { value: 'req' } });
  await waitFor(() => expect(screen.getByRole('button', { name: '编辑验收条款' })).toHaveProperty('disabled', false));
  fireEvent.click(screen.getByRole('button', { name: '编辑验收条款' }));
  fireEvent.click(screen.getByRole('button', { name: '保存验收条款' }));
  await waitFor(() => expect(write).toHaveBeenCalledWith('/tests/acceptance', expect.anything()));
  fireEvent.click(screen.getByRole('button', { name: '测试条目' }));
  const checkbox = await screen.findByRole('checkbox', { name: 'Updated payment once' });
  expect(checkbox).toHaveProperty('checked', false);
  fireEvent.click(checkbox);
  fireEvent.click(screen.getByRole('button', { name: '保存条目' }));
  await waitFor(() => expect(write).toHaveBeenCalledWith('/tests/cases', expect.objectContaining({
    case: expect.objectContaining({ coverage: [{ requirement_id: 'req', revision: 1, plan_version: 2, criterion_id: 'once' }] }),
  })));
});
