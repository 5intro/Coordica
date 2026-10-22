import { expect, it } from 'vitest';
import { ApiClient } from './client';
import { InitializationAnswers } from './initializationAnswers';

const first = { key: 'original-key', body: {
  expected_version: 2, question_id: 'architecture', original_text: '私有原文',
  source: { session_id: 'web', message_id: 'original-message' },
} };

it('keeps answer prose and identity isolated by project and credential session', () => {
  const client = new ApiClient('first-credential');
  const session = client.initializationAnswers;
  session.begin('first-project', first);
  expect(session.read('first-project')?.pending).toEqual(first);
  expect(session.read('other-project')).toBeUndefined();
  expect(new ApiClient('other-credential').initializationAnswers.read('first-project')).toBeUndefined();
  session.settle('other-project', first.key, true);
  expect(session.read('first-project')?.pending).toEqual(first);
});

it('preserves uncertain identity and rejected prose, and fences late receipts', () => {
  const session = new InitializationAnswers();
  session.begin('project', first);
  const second = { ...first, key: 'new-key', body: { ...first.body, original_text: '新回答' } };
  expect(session.begin('project', second)).toBe(first);
  session.edit('project', { version: 3, question: '', text: '不得覆盖未确认请求' });
  expect(session.read('project')?.draft.text).toBe('私有原文');
  session.settle('project', first.key, false);
  expect(session.read('project')).toMatchObject({ draft: { text: '私有原文', version: 2 }, pending: null });
  session.begin('project', second);
  session.settle('project', first.key, true);
  expect(session.read('project')?.pending).toBe(second);
  session.settle('project', second.key, true);
  expect(session.read('project')).toBeUndefined();
});
