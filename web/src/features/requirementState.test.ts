import { expect, it } from 'vitest';
import type { Requirement, Task } from '../api.generated';
import { activeRequirement } from './requirementState';
it('uses current content work instead of completed historical work for activity', () => {
  const requirement = { id: 'req', content_revision: 2 } as Requirement;
  const previous = { requirement_ids: ['req'], requirement_revisions: { req: 1 }, state: 'integrated' } as unknown as Task;
  const current = { requirement_ids: ['req'], requirement_revisions: { req: 2 }, state: 'integrated' } as unknown as Task;
  expect(activeRequirement(requirement, [previous])).toBe(true);
  expect(activeRequirement(requirement, [previous, current])).toBe(false);
  expect(activeRequirement({ ...requirement, state: 'withdrawn' }, [])).toBe(false);
});
