import type { Initialization, Project } from '../../api.generated';
import type { ApiClient } from '../../api/client';
import type { useAction } from '../../hooks/useAction';

export type PreparationAction = ReturnType<typeof useAction>;
export type PreparationControls = { client: ApiClient; action: PreparationAction };
export type PreparationState = { project: Project; current: Initialization | null };
