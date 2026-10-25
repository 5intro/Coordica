import type { Baseline, Initialization } from '../api.generated';

const strings = (v: unknown): v is string[] => Array.isArray(v) && v.every((s) => typeof s === 'string');
const object = (v: unknown): v is Record<string, unknown> => !!v && typeof v === 'object' && !Array.isArray(v);
function baseline(v: unknown): v is Baseline {
  if (!object(v) || !(['requirements', 'acceptance', 'architecture', 'modules', 'conventions', 'testing', 'functionality', 'risks'] as const).every((key) => typeof v[key] === 'string') || !strings(v.allowed_roots) || !strings(v.protected_paths) || !Array.isArray(v.checks)) return false;
  return v.checks.every((c: unknown) => object(c) && typeof c.name === 'string' && typeof c.command === 'string' && strings(c.args) && Number.isSafeInteger(c.timeout_seconds) && Number(c.timeout_seconds) >= 1 && Number(c.timeout_seconds) <= 3600);
}

// Validate the versioned boundary before enabling Owner confirmation controls.
export function validInitialization(value: unknown): value is Initialization {
  if (!value || typeof value !== 'object') return false;
  const v = value as Partial<Initialization>;
  if (!Number.isSafeInteger(v.version) || !['draft', 'analyzing', 'interview', 'awaiting_owner', 'initialized'].includes(v.status ?? '')
    || typeof v.digest !== 'string' || typeof v.source_sha !== 'string'
    || !strings(v.available_paths) || !strings(v.covered_paths) || !strings(v.omissions) || !Array.isArray(v.feedback) || !v.feedback.every((f: unknown) => object(f) && typeof f.id === 'string' && Number.isSafeInteger(f.version) && typeof f.actor_id === 'string' && typeof f.question_id === 'string' && typeof f.original_text === 'string' && typeof f.created_at === 'string' && object(f.source) && typeof f.source.session_id === 'string' && typeof f.source.message_id === 'string')) return false;
  if (v.result === null) return v.status === 'draft' || v.status === 'analyzing';
  const r = v.result;
  if (!r || v.status !== 'analyzing' && (r.generation !== v.operation_generation || r.initialization_version !== v.version || r.operation_id !== v.operation_id || r.source_sha !== v.source_sha) || typeof r.ready !== 'boolean' || !baseline(r.baseline) || !Array.isArray(r.facts) || !r.facts.every((f: unknown) => object(f) && typeof f.path === 'string' && typeof f.fact === 'string') || !strings(r.inspected_paths) || !Array.isArray(r.questions) || !r.questions.every((q: unknown) => object(q) && typeof q.id === 'string' && typeof q.text === 'string' && strings(q.prerequisites)) || !strings(r.suggestions)) return false;
  return !(v.status === 'awaiting_owner' || v.status === 'initialized') || r.ready && r.questions.length === 0 && /^[a-f0-9]{64}$/.test(v.digest);
}

// Go encodes the new-project worker's nil omissions slice as null. Normalize
// only this known wire representation, without changing persisted digest input
// or weakening the strict contract for any other field.
export function initializationReply(value: unknown): Initialization | null {
  const normalized = object(value) && value.omissions === null ? { ...value, omissions: [] } : value;
  return validInitialization(normalized) ? normalized : null;
}
