// Verification completes without creating or integrating a code candidate.
export const completedTask = (state: string) => ['integrated', 'verified'].includes(state);
export const terminalTask = (state: string) => completedTask(state) || state === 'cancelled';
