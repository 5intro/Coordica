import { useCallback, useEffect, useRef, type CSSProperties } from 'react';
import { Glass } from './Glass';
import { Icon, type IconName } from './icons';
import './material.css';

export type DockItem = { id: string; name: string; icon: IconName };

export function GlassDock({ active, onSelect, appearance, items, label = '工作台导航' }: { active: string; onSelect: (id: string) => void; appearance: { motion: boolean; refraction: boolean }; items: DockItem[]; label?: string }) {
  const root = useRef<HTMLElement>(null);
  const frame = useRef(0);
  const pointer = useRef<number | null>(null);
  const magnify = useCallback((clientX: number | null) => {
    pointer.current = clientX;
    cancelAnimationFrame(frame.current);
    frame.current = requestAnimationFrame(() => {
      const element = root.current;
      if (!element) return;
      const dock = element.getBoundingClientRect();
      const slots = Array.from(element.querySelectorAll<HTMLDivElement>('.dock-slot'));
      const compact = window.matchMedia?.('(max-width: 620px)').matches;
      const samples = slots.map((slot) => {
        // Measure the fixed slot, never the animated button: hover stays stable.
        const bounds = slot.getBoundingClientRect();
        const distance = clientX === null ? 10000 : Math.abs(clientX - bounds.left - bounds.width / 2);
        const influence = appearance.motion && !compact ? Math.exp(-(distance ** 2) / (76 ** 2)) : 0;
        return { influence, extra: bounds.width * influence * .30 };
      });
      const expansion = samples.reduce((sum, sample) => sum + sample.extra, 0);
      const peak = Math.max(0, ...samples.map((sample) => sample.influence));
      element.style.setProperty('--pointer-x', clientX === null ? '50%' : `${Math.max(0, Math.min(100, (clientX - dock.left) / dock.width * 100))}%`);
      element.style.setProperty('--dock-stretch-x', String(1 + expansion / dock.width));
      element.style.setProperty('--dock-stretch-y', String(1 + peak * .09));
      element.style.setProperty('--dock-rise', `${-peak * 1.5}px`);
      let offset = -expansion / 2;
      slots.forEach((slot, index) => {
        const sample = samples[index];
        const shift = offset + sample.extra / 2;
        const button = slot.querySelector<HTMLButtonElement>('.dock-item');
        const targets = [button, ...(items[index].id === active ? [element.querySelector<HTMLDivElement>('.dock-selection')] : [])];
        targets.forEach((target) => {
          target?.style.setProperty('--zoom', String(1 + sample.influence * .30));
          target?.style.setProperty('--lift', `${-sample.influence * 13}px`);
          target?.style.setProperty('--shift', `${shift}px`);
        });
        offset += sample.extra;
      });
    });
  }, [active, appearance.motion, items]);
  useEffect(() => {
    magnify(pointer.current);
    const reset = () => magnify(null);
    window.addEventListener('resize', reset);
    return () => window.removeEventListener('resize', reset);
  }, [magnify]);
  useEffect(() => () => cancelAnimationFrame(frame.current), []);
  return <nav className="glass-dock" aria-label={label} ref={root}
    style={{ '--dock-count': Math.max(1, items.length) } as CSSProperties}
    onPointerMove={(event) => { if (event.pointerType !== 'touch') magnify(event.clientX); }}
    onPointerLeave={() => magnify(null)} onKeyDown={(event) => {
    if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return;
    const buttons = Array.from(event.currentTarget.querySelectorAll<HTMLButtonElement>('.dock-item'));
    const index = buttons.indexOf(document.activeElement as HTMLButtonElement);
    const next = event.key === 'Home' ? 0 : event.key === 'End' ? buttons.length - 1 : (index + (event.key === 'ArrowRight' ? 1 : -1) + buttons.length) % buttons.length;
    buttons[next]?.focus(); event.preventDefault();
  }}>
    <Glass className="dock-glass" radius={29} refract={appearance.refraction}>
      <div className="dock-track"><Glass className="dock-selection" radius={19} refract={appearance.refraction} style={{ '--dock-index': Math.max(0, items.findIndex((item) => item.id === active)), '--after-divider': items.findIndex((item) => item.id === active) >= 5 ? 1 : 0 } as CSSProperties}><span/></Glass>{items.map((item, index) => <div className={`dock-slot ${index === 5 ? 'dock-separated' : ''}`} key={item.id}>
        <button className={`dock-item ${active === item.id ? 'is-active' : ''}`} aria-label={item.name} aria-current={active === item.id ? 'page' : undefined} onClick={() => onSelect(item.id)} onFocus={(event) => { const b = event.currentTarget.parentElement!.getBoundingClientRect(); magnify(b.left + b.width / 2); }} onBlur={() => magnify(null)}>
          <span className="dock-tooltip">{item.name}</span><span className="glyph-large"><Icon name={item.icon} size={25}/></span><span className="dock-active-dot"/>
        </button>
      </div>)}</div>
    </Glass>
  </nav>;
}
