import { expect, it } from 'vitest';
import { ApiClient } from './client';

it('isolates policy drafts by project and credentials and fences late receipts', () => {
  const client = new ApiClient('owner');
  const session = client.initializationPolicies;
  const draft = { version: 1, checks: '[private]', protectedPaths: ['private/**'] };
  const first = { key: 'first', body: { expected_version: 1, checks: [], protected_paths: ['private/**'] } };
  session.begin('project', draft, first);
  expect(session.read('another')).toBeUndefined();
  expect(new ApiClient('other').initializationPolicies.read('project')).toBeUndefined();
  session.edit('project', { ...draft, checks: 'cannot overwrite pending' });
  expect(session.read('project')?.draft).toBe(draft);
  session.settle('project', first.key, false);
  expect(session.read('project')).toMatchObject({ draft, pending: null });
  const second = { ...first, key: 'second' };
  session.begin('project', draft, second);
  session.settle('project', first.key, true);
  expect(session.read('project')?.pending).toBe(second);
  session.settle('project', second.key, true);
  expect(session.read('project')).toBeUndefined();
});
