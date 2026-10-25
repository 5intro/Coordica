import { useEffect, useRef, useState } from 'react';
import type { Page } from '../App';
import { routeEvent } from './useRouteField';

function hashFocus() { return new URLSearchParams(window.location.hash.split('?')[1] ?? '').get('id') ?? undefined; }
function hashPage(pages: readonly { id: Page }[]): Page | undefined {
  const hash = window.location.hash.replace(/^#\/?/, '').split('?')[0];
  return pages.find((item) => item.id === hash)?.id;
}

export function useWorkspaceRoute(pages: readonly { id: Page }[]) {
  const [page, setPage] = useState<Page>(() => hashPage(pages) ?? 'overview');
  const [focusId, setFocusId] = useState<string | undefined>(hashFocus);
  const [visited, setVisited] = useState<Set<Page>>(() => new Set([hashPage(pages) ?? 'overview']));
  const positions = useRef<Partial<Record<Page, number>>>({});
  const currentPage = useRef(page);
  const locations = useRef<Partial<Record<Page, string>>>({ [page]: window.location.hash });
  useEffect(() => {
    let frame: number | undefined;
    function change() {
      if (frame !== undefined) cancelAnimationFrame(frame);
      const next = hashPage(pages) ?? 'overview';
      locations.current[next] = window.location.hash;
      currentPage.current = next;
      setPage(next); setFocusId(hashFocus());
      setVisited((previous) => new Set([...previous, next]));
      frame = requestAnimationFrame(() => window.scrollTo({
        top: window.history.state?.scroll ?? positions.current[next] ?? 0, behavior: 'auto',
      }));
    }
    function remember() {
      positions.current[currentPage.current] = window.scrollY;
      window.history.replaceState({ ...window.history.state, scroll: window.scrollY }, '');
    }
    for (const event of ['hashchange', 'popstate', routeEvent]) window.addEventListener(event, change);
    window.addEventListener('scroll', remember, { passive: true });
    return () => {
      for (const event of ['hashchange', 'popstate', routeEvent]) window.removeEventListener(event, change);
      window.removeEventListener('scroll', remember);
      if (frame !== undefined) cancelAnimationFrame(frame);
    };
  }, []);
  function navigate(next: Page, id?: string) {
    positions.current[page] = window.scrollY;
    window.history.replaceState({ ...window.history.state, scroll: window.scrollY }, '');
    const hash = !id && locations.current[next] ? locations.current[next]! : `#${next}${id ? `?id=${encodeURIComponent(id)}` : ''}`;
    if (window.location.hash !== hash) window.history.pushState({ scroll: positions.current[next] ?? 0 }, '', hash);
    // The dock can be clicked before the first passive route listener mounts.
    // Apply local navigation immediately; the event still restores URL fields
    // and scroll through the same shell path used by browser history.
    currentPage.current = next;
    locations.current[next] = hash;
    setPage(next); setFocusId(hashFocus());
    setVisited((previous) => new Set([...previous, next]));
    window.dispatchEvent(new Event(routeEvent));
  }
  return { page, focusId, visited, navigate };
}
