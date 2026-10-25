import { expect, it } from 'vitest';
import { acceptedAnswer } from '../test/answerReceipt';
import { answerReceipt } from './answerReceipt';

const request = { expected_version: 2, question_id: 'architecture', original_text: '原始回答',
  source: { session_id: 'web', message_id: 'original' } };

it('binds accepted answers to exact version, source, question and original prose', () => {
  const good = acceptedAnswer(request);
  expect(answerReceipt(good, request)).toBeTruthy();
  expect(answerReceipt({ ...good, version: 4 }, request)).toBeUndefined();
  for (const change of [
    { version: 3 }, { original_text: 'another answer' }, { question_id: 'different question' },
    { source: { ...request.source, message_id: 'other' } },
  ]) {
    const feedback = { ...good.feedback!.at(-1)!, ...change };
    expect(answerReceipt({ ...good, feedback: [feedback] }, request)).toBeUndefined();
  }
});
