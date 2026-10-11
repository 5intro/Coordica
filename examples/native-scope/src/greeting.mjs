/** Baseline source for the native scope bridge fixture. */
export function greeting(name = '') {
  return `Hello, ${name || 'guest'}!`;
}
