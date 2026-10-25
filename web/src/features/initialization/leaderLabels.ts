import type { ModelOption } from '../../api.generated';

// Formatting only: the original opaque ID is always sent unchanged. ACP tools
// may report the same display name for different provider accounts.
export function leaderModelLabel(id: string, models: ModelOption[] = []): string {
  const model = models.find((item) => item.id === id);
  if (model?.name && models.filter((item) => item.name === model.name).length === 1) return model.name;
  try {
    const pair: unknown = JSON.parse(id);
    if (Array.isArray(pair) && pair.length === 2 && pair.every((item) => typeof item === 'string')) {
      return `${model?.name || pair[1]} · ${pair[0]}`;
    }
  } catch { /* Non-JSON identifiers retain their tool-reported label. */ }
  return model?.name || id;
}
