import type { Initialization, InitializationAnswer } from '../api.generated';
import { readyInitialization } from './initialization';

export function acceptedAnswer(body: unknown): Initialization {
  const request = body as InitializationAnswer;
  return { ...readyInitialization, version: request.expected_version + 1, status: 'analyzing',
    digest: '', operation_id: 'next-operation', operation_state: 'queued', operation_version: 1,
    feedback: [...readyInitialization.feedback!, { id: 'accepted-feedback', version: request.expected_version,
      actor_id: 'owner', question_id: request.question_id, original_text: request.original_text,
      source: { ...request.source, client_id: request.source.client_id || 'owner' }, created_at: '' }],
  };
}
