import { useEffect, useRef, type ReactNode } from 'react';
import { Glass } from './Glass';
import { Icon } from './icons';

/** A nonmodal inspection surface. The source list remains available behind it. */
export function DetailPanel({ title, children, onClose, back }: { title: string; children: ReactNode; onClose: () => void; back?: () => void }) {
  const close = useRef<HTMLButtonElement>(null);
  const panel = useRef<HTMLElement>(null);
  const origin = useRef(document.activeElement as HTMLElement | null);
  const onCloseRef = useRef(onClose);
  onCloseRef.current = onClose;
  useEffect(() => {
    const frame = requestAnimationFrame(() => {
      if (!panel.current?.contains(document.activeElement)) close.current?.focus();
    });
    const escape = (event: KeyboardEvent) => { if (event.key === 'Escape') { event.stopPropagation(); onCloseRef.current(); } };
    document.addEventListener('keydown', escape);
    return () => { cancelAnimationFrame(frame); document.removeEventListener('keydown', escape); if (origin.current?.isConnected) origin.current.focus({ preventScroll: true }); };
  }, []);
  return <><div className="inspection-scrim" aria-hidden="true" onClick={onClose}/><aside ref={panel} className="inspection-panel" role="dialog" aria-label={title}>
    <Glass className="inspection-glass" radius={27}>
      <header className="inspection-tools">{back ? <button className="inspection-icon" aria-label="返回上一项" onClick={back}><Icon name="back" size={18}/></button> : <span/>}<button ref={close} className="inspection-icon" aria-label="关闭详情面板" onClick={onClose}><Icon name="close" size={18}/></button></header>
      <div className="inspection-content">{children}</div>
      <footer className="inspection-footer">Esc 关闭</footer>
    </Glass>
  </aside></>;
}
