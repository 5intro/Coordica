import { useEffect, useState } from 'react';

export const routeEvent = 'coordica-route';
export function routeParams() { return new URLSearchParams(window.location.hash.split('?')[1] ?? ''); }
export function routeName() { return window.location.hash.replace(/^#\/?/, '').split('?')[0]; }
// Local state also works when a feature is rendered independently. In the shell,
// every filter/selection has a shareable URL and browser history entry.
export function useRouteField(page: string, name: string, fallback = '') {
  const read = () => routeParams().get(name) ?? fallback;
  const [value, setValue] = useState(() => routeName() === page ? read() : fallback);
  useEffect(() => {
    function restore() { if (routeName() === page) setValue(read()); }
    for (const event of ['hashchange', 'popstate', routeEvent]) window.addEventListener(event, restore);
    return () => { for (const event of ['hashchange', 'popstate', routeEvent]) window.removeEventListener(event, restore); };
  }, [page, name, fallback]);
  function update(next: string, remove: string[] = []) {
    setValue(next);
    if (routeName() !== page) return;
    const params = routeParams();
    remove.forEach((key) => params.delete(key));
    if (next === fallback && name !== 'id') params.delete(name); else params.set(name, next);
    const query = params.toString();
    window.history.replaceState({ ...window.history.state, scroll: window.scrollY }, '');
    window.history.pushState({ scroll: window.scrollY }, '', `#${page}${query ? `?${query}` : ''}`);
    window.dispatchEvent(new Event(routeEvent));
  }
  return [value, update] as const;
}
