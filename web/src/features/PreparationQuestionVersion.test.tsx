import { acceptedAnswer } from '../test/answerReceipt';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import { ApiClient } from '../api/client';
import { readyInitialization } from '../test/initialization';
import { preparationSnapshot as snapshot } from '../test/preparation';
import { ProjectInitialization } from './ProjectInitialization';

it.each([true, false])('pins a selected-only question after route remount, reused ID: %s', async (reusedID) => {
  const client = new ApiClient('owner');
  const write = vi.spyOn(client, 'write').mockImplementation(async (_path, body) => acceptedAnswer(body));
  const question = { id: 'architecture', text: '原先问题', prerequisites: [] };
  const original = { ...readyInitialization, status: 'interview', result: {
    ...readyInitialization.result!, questions: [question], ready: false,
  } };
  const props = { client, refresh: vi.fn() };
  const view = render(<ProjectInitialization {...props} snapshot={{ ...snapshot, initialization: original }} />);
  fireEvent.click(screen.getByRole('button', { name: '回答此问题' }));
  view.unmount();
  const current = { ...original, version: 3, result: {
    ...original.result, initialization_version: 3,
    questions: reusedID ? [{ ...question, text: '改过内容的新问题' }] : [],
  } };
  render(<ProjectInitialization {...props} snapshot={{ ...snapshot, initialization: current }} />);
  expect(screen.getByRole('button', { name: '核对当前问题版本' })).toBeTruthy();
  fireEvent.change(screen.getByLabelText('原始回答 / 反馈'), { target: { value: '对原先问题的回答' } });
  fireEvent.click(screen.getByRole('button', { name: '保存原文并继续访谈' }));
  expect(write).not.toHaveBeenCalled();
  expect(screen.getByRole('alert').textContent).toContain('问题版本已变化');
  fireEvent.click(screen.getByRole('button', { name: '核对当前问题版本' }));
  fireEvent.click(screen.getByRole('button', { name: '保存原文并继续访谈' }));
  await waitFor(() => expect(write).toHaveBeenCalledWith('/initialization/answer', expect.objectContaining({
    expected_version: 3, question_id: reusedID ? question.id : '', original_text: '对原先问题的回答',
  }), expect.any(String)));
});
