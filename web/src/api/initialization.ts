import type { Baseline, Initialization } from '../api.generated';

const strings = (value: unknown): value is string[] =>
  Array.isArray(value) && value.every((item) => typeof item === 'string');
const object = (value: unknown): value is Record<string, unknown> =>
  !!value && typeof value === 'object' && !Array.isArray(value);

function baseline(value: unknown): value is Baseline {
  const fields = ['requirements', 'acceptance', 'architecture', 'modules',
    'conventions', 'testing', 'functionality', 'risks'] as const;
  if (!object(value) || !fields.every((key) => typeof value[key] === 'string')
    || !strings(value.allowed_roots) || !strings(value.protected_paths) || !Array.isArray(value.checks)) return false;
  return value.checks.every((check: unknown) => object(check) && typeof check.name === 'string'
    && typeof check.command === 'string' && strings(check.args) && Number.isSafeInteger(check.timeout_seconds)
    && Number(check.timeout_seconds) >= 1 && Number(check.timeout_seconds) <= 3600);
}

function feedback(value: unknown): boolean {
  return object(value) && typeof value.id === 'string' && Number.isSafeInteger(value.version)
    && typeof value.actor_id === 'string' && typeof value.question_id === 'string'
    && typeof value.original_text === 'string' && typeof value.created_at === 'string'
    && object(value.source) && typeof value.source.session_id === 'string'
    && typeof value.source.message_id === 'string';
}

function result(value: unknown): boolean {
  if (!object(value) || typeof value.ready !== 'boolean' || !baseline(value.baseline)
    || !strings(value.inspected_paths) || !strings(value.suggestions)
    || !Array.isArray(value.facts) || !Array.isArray(value.questions)) return false;
  return value.facts.every((fact: unknown) => object(fact)
    && typeof fact.path === 'string' && typeof fact.fact === 'string')
    && value.questions.every((question: unknown) => object(question)
      && typeof question.id === 'string' && typeof question.text === 'string' && strings(question.prerequisites));
}

function confirmed(value: Partial<Initialization>): boolean {
  return value.status !== 'initialized' || Number(value.version) > 0
    && value.approved_version === value.version && value.approved_digest === value.digest
    && baseline(value.approved_baseline);
}

// Validate the versioned boundary before enabling Owner confirmation controls.
export function validInitialization(value: unknown): value is Initialization {
  if (!object(value)) return false;
  const current = value as Partial<Initialization>;
  const statuses = ['draft', 'analyzing', 'interview', 'awaiting_owner', 'initialized'];
  if (!Number.isSafeInteger(current.version) || !statuses.includes(current.status ?? '')
    || typeof current.digest !== 'string' || typeof current.source_sha !== 'string'
    || !strings(current.available_paths) || !strings(current.covered_paths) || !strings(current.omissions)
    || !Array.isArray(current.feedback) || !current.feedback.every(feedback) || !confirmed(current)) return false;
  if (current.result === null) return current.status === 'draft' || current.status === 'analyzing';
  const answer = current.result;
  if (!answer || !result(answer)) return false;
  if (current.status !== 'analyzing' && (answer.generation !== current.operation_generation
    || answer.initialization_version !== current.version || answer.operation_id !== current.operation_id
    || answer.source_sha !== current.source_sha)) return false;
  return !(current.status === 'awaiting_owner' || current.status === 'initialized')
    || answer.ready && answer.questions?.length === 0 && /^[a-f0-9]{64}$/.test(current.digest);
}
