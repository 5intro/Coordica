import type { ReactNode } from 'react';

/** Pool pages keep selection, evidence and actions alongside a single list. */
export function PoolInspection({ title, children }: { title: string; children: ReactNode }) {
  return <aside className="pool-inspector" aria-label={title}>{children}</aside>;
}
