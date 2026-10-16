import type { CSSProperties } from 'react';

export type IconName = 'home' | 'idea' | 'task' | 'review' | 'block' | 'agents' | 'test' | 'settings' | 'arrow' | 'close' | 'plus' | 'search' | 'play' | 'pause' | 'check' | 'chevron' | 'back' | 'more' | 'mac' | 'windows' | 'code' | 'spark' | 'clock' | 'link' | 'sun' | 'activity' | 'send' | 'volume';

const paths: Record<IconName, React.ReactNode> = {
  home: <><rect x="3" y="3" width="7" height="7" rx="2"/><rect x="14" y="3" width="7" height="7" rx="2"/><rect x="3" y="14" width="7" height="7" rx="2"/><rect x="14" y="14" width="7" height="7" rx="2"/></>,
  idea: <><path d="M8 15.5C8 13 5.5 12.3 5.5 8.7a6.5 6.5 0 0 1 13 0c0 3.6-2.5 4.3-2.5 6.8Z"/><path d="M9 19h6m-5 3h4M9.5 9.5l2.5 2 2.5-2M12 11.5v4"/></>,
  task: <><rect x="5" y="3" width="14" height="18" rx="3"/><path d="m8.5 9 1.5 1.5 3-3m-4.5 8h7M13 10h2.5"/></>,
  review: <><path d="M12 2.5 20 6v6c0 4.2-3.9 7.8-8 9.5C7.9 19.8 4 16.2 4 12V6Z"/><path d="m8.5 11.5 2.5 2.5 4.5-5"/></>,
  block: <><path d="m9.4 4.4-7 12.1A2.4 2.4 0 0 0 4.5 20h15a2.4 2.4 0 0 0 2.1-3.5l-7-12.1a3 3 0 0 0-5.2 0Z"/><path d="M12 8v5.5m0 3v.1"/></>,
  agents: <><circle cx="9" cy="8" r="3.5"/><path d="M2.5 20v-2a6.5 6.5 0 0 1 13 0v2m1-15.3a3.5 3.5 0 0 1 0 6.6M19 14a6 6 0 0 1 3 5"/></>,
  test: <><path d="M9 3h6m-5 0v7l-6 9a1.3 1.3 0 0 0 1.1 2h13.8a1.3 1.3 0 0 0 1.1-2l-6-9V3M7 15h10"/><path d="m10 17 1.2 1.2 2.5-2.5"/></>,
  settings: <><path d="m9.5 3-.7 2.3-2 .9-2.3-.6-2 3.5 1.6 1.7v2.4l-1.6 1.7 2 3.5 2.3-.6 2 .9.7 2.3h4.1l.7-2.3 2-.9 2.3.6 2-3.5-1.6-1.7v-2.4l1.6-1.7-2-3.5-2.3.6-2-.9-.7-2.3Z"/><circle cx="11.5" cy="12" r="3.2"/></>,
  arrow: <><path d="M5 12h14m-5-5 5 5-5 5"/></>,
  chevron: <path d="m9 5 7 7-7 7"/>,
  close: <path d="m6 6 12 12M6 18 18 6"/>,
  plus: <path d="M12 5v14M5 12h14"/>,
  search: <><circle cx="10.5" cy="10.5" r="6.5"/><path d="m15.5 15.5 5 5"/></>,
  play: <path d="M8 4.5 20 12 8 19.5Z"/>,
  pause: <><path d="M8 5v14M16 5v14"/></>,
  check: <path d="m5 12 4.5 4.5L19 7"/>,
  back: <path d="m14 5-7 7 7 7"/>,
  more: <><circle cx="5" cy="12" r="1"/><circle cx="12" cy="12" r="1"/><circle cx="19" cy="12" r="1"/></>,
  mac: <><rect x="4" y="3" width="16" height="12" rx="2"/><path d="M9 19h6m-3-4v4M2 22h20"/></>,
  windows: <><rect x="3" y="3" width="8" height="8" rx="1"/><rect x="14" y="3" width="7" height="8" rx="1"/><rect x="3" y="14" width="8" height="7" rx="1"/><rect x="14" y="14" width="7" height="7" rx="1"/></>,
  code: <><path d="m8 7-5 5 5 5m8-10 5 5-5 5m-3-14-2 18"/></>,
  spark: <><path d="m12 3 2.4 6.6L21 12l-6.6 2.4L12 21l-2.4-6.6L3 12l6.6-2.4Z"/></>,
  clock: <><circle cx="12" cy="12" r="9"/><path d="M12 7v5l3.5 2"/></>,
  link: <><path d="m10 8 3-3a4.3 4.3 0 0 1 6 6l-3 3m-2 2-3 3a4.3 4.3 0 0 1-6-6l3-3m1 5 6-6"/></>,
  sun: <><circle cx="12" cy="12" r="4"/><path d="M12 2v2m0 16v2M2 12h2m16 0h2M5 5l1.5 1.5m11 11L19 19M5 19l1.5-1.5m11-11L19 5"/></>,
  activity: <path d="M2 12h4l3-7 5 14 3-7h5"/>,
  send: <><path d="m3 3 18 9-18 9 4-9Zm4 9h14"/></>,
  volume: <><path d="m11 5-5 4H3v6h3l5 4Zm4 3a6 6 0 0 1 0 8m3-11a10 10 0 0 1 0 14"/></>,
};

export function Icon({ name, size = 20, style, className = '' }: { name: IconName; size?: number; style?: CSSProperties; className?: string }) {
  return <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" style={style} className={className}>{paths[name]}</svg>;
}
