import type { Initialization, InitializationAnswer } from '../api.generated';
import { initializationReply } from './initialization';

export function answerReceipt(value: unknown, request: InitializationAnswer): Initialization | undefined {
  const result = initializationReply(value);
  if (!result || result.version !== request.expected_version + 1) return undefined;
  const received = result.feedback?.filter((item) => item.source.session_id === request.source.session_id
    && item.source.message_id === request.source.message_id);
  if (received?.length !== 1) return undefined;
  const item = received[0];
  return item.version === request.expected_version && item.question_id === request.question_id
    && item.original_text === request.original_text
    && (!request.source.client_id || item.source.client_id === request.source.client_id) ? result : undefined;
}
