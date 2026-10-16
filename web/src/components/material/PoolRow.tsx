import { useId, useRef, useState, type CSSProperties, type ReactNode } from 'react';

const MIN_HEIGHT = 112, MAX_HEIGHT = 560;
const bound = (height: number) => Math.round(Math.max(MIN_HEIGHT, Math.min(MAX_HEIGHT,height)));
/** Presentation-only row sizing. It cannot change project or scheduling state. */
export function PoolRow({ row, count, children, storageKey = 'coordica.workbench.pool-height' }: { row: number; count: number; children: ReactNode; storageKey?: string }) {
  const key = `${storageKey}.${row}`;
  const [height,setHeight] = useState<number | undefined>(() => {
    try { const saved = localStorage.getItem(key); const value = saved === null ? NaN : Number(saved); return Number.isFinite(value) && value >= MIN_HEIGHT && value <= MAX_HEIGHT ? value : undefined; } catch { return undefined; }
  });
  const [dragging,setDragging] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  const origin = useRef<{ y: number; height: number } | null>(null);
  const help = useId();
  function currentHeight() { return root.current?.querySelector('.pool-well')?.getBoundingClientRect().height || 206; }
  function resize(value: number | undefined, persist = true) {
    const next = value === undefined ? undefined : bound(value);
    setHeight(next);
    if (!persist) return;
    try { if (next === undefined) localStorage.removeItem(key); else localStorage.setItem(key,String(next)); } catch { /* A view preference is optional. */ }
  }
  return <div className={`pool-row ${dragging ? 'is-resizing' : ''}`} ref={root} style={{ '--pool-row-height':height === undefined ? undefined : `${height}px`, '--preview-count':Math.max(2,Math.min(3,count)) } as CSSProperties}>
    {children}
    <div className="pool-row-resize" role="separator" tabIndex={0} aria-label={`调整第 ${row + 1} 行工作池高度`} aria-orientation="horizontal" aria-valuemin={MIN_HEIGHT} aria-valuemax={MAX_HEIGHT} aria-valuenow={height ?? 206} aria-valuetext={height === undefined ? '默认高度' : `${height} 像素`} aria-describedby={help}
      onPointerDown={(event) => { if (event.button !== 0) return; event.preventDefault(); event.currentTarget.focus({preventScroll:true}); event.currentTarget.setPointerCapture(event.pointerId); origin.current={y:event.clientY,height:currentHeight()}; setDragging(true); }}
      onPointerMove={(event) => { if (origin.current) resize(origin.current.height + event.clientY - origin.current.y, false); }}
      onPointerUp={(event) => { if (origin.current) resize(origin.current.height + event.clientY - origin.current.y); origin.current=null; setDragging(false); if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId); }}
      onPointerCancel={() => { origin.current=null; setDragging(false); }}
      onLostPointerCapture={() => { origin.current=null; setDragging(false); }}
      onDoubleClick={() => resize(undefined)}
      onKeyDown={(event) => { if (['ArrowUp','ArrowDown','Home','End'].includes(event.key)) { event.preventDefault(); resize(event.key === 'Home' ? undefined : event.key === 'End' ? MAX_HEIGHT : (height ?? currentHeight()) + (event.key === 'ArrowDown' ? 16 : -16)); } }}><span/></div>
    <span id={help} className="sr-only">拖动调整本行高度。上下方向键微调，Home 恢复默认，End 调至最大；双击恢复默认。</span>
  </div>;
}
