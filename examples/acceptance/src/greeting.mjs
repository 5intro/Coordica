/** Baseline behavior: a name produces a greeting; empty input uses a guest. */
export function greeting(name = '') {
  return `Hello, ${name || 'guest'}!`;
}
