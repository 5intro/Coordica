import { useId, useLayoutEffect, useRef, useState, type CSSProperties, type ReactNode } from 'react';
import { DISPLACEMENT_SCALE, lensOffset } from './glassOptics';

const TILE = 32;
let tiles: string[] | undefined;

/** Nine shared tiles preserve rounded edges at any surface size. Raster work
 * stays fixed at 96 × 96 pixels for the whole application, including resize. */
function lensTiles(): string[] {
  if (tiles) return tiles;
  const canvas = document.createElement('canvas');
  canvas.width = canvas.height = TILE;
  const context = canvas.getContext('2d');
  if (!context) return [];
  const result: string[] = [];
  for (let row = 0; row < 3; row++) for (let column = 0; column < 3; column++) {
    const pixels = context.createImageData(TILE, TILE);
    for (let y = 0; y < TILE; y++) for (let x = 0; x < TILE; x++) {
      const offset = lensOffset(column * TILE + x + .5, row * TILE + y + .5, TILE * 3, TILE * 3, TILE);
      const index = (y * TILE + x) * 4;
      pixels.data[index] = 128 + offset.x / DISPLACEMENT_SCALE * 255;
      pixels.data[index + 1] = 128 + offset.y / DISPLACEMENT_SCALE * 255;
      pixels.data[index + 2] = 128;
      pixels.data[index + 3] = 255;
    }
    context.putImageData(pixels, 0, 0);
    result.push(canvas.toDataURL());
  }
  tiles = result;
  return result;
}

export function Glass({ children, className = '', radius = 28, blur = .65, refract = true, style }: { children: ReactNode; className?: string; radius?: number; blur?: number; refract?: boolean; style?: CSSProperties }) {
  const id = `glass-${useId().replace(/[^a-zA-Z0-9]/g, '')}`;
  const ref = useRef<HTMLDivElement>(null);
  const [map, setMap] = useState({ tiles: [] as string[], width: 1, height: 1 });
  // Parsing url() alone does not detect SVG backdrop rendering support in WebKit.
  // Keep the known enhanced engine explicit and give other engines real blur.
  const enhanced = refract && /(?:Chrome|Chromium|Edg)\//.test(navigator.userAgent);
  useLayoutEffect(() => {
    const element = ref.current;
    if (!element || !enhanced) return;
    const observer = new ResizeObserver(([entry]) => {
      const width = Math.max(1, Math.ceil(entry.contentRect.width));
      const height = Math.max(1, Math.ceil(entry.contentRect.height));
      const shared = lensTiles();
      setMap((old) => old.width === width && old.height === height && old.tiles === shared ? old : { tiles: shared, width, height });
    });
    observer.observe(element);
    return () => observer.disconnect();
  }, [radius, enhanced]);
  const corner = Math.max(0, Math.min(radius, map.width / 2, map.height / 2));
  const xs = [0, corner, map.width - corner], ys = [0, corner, map.height - corner];
  const widths = [corner, map.width - 2 * corner, corner], heights = [corner, map.height - 2 * corner, corner];
  return <div ref={ref} className={`glass ${className}`} style={{ ...style, '--glass-radius': `${radius}px`, '--glass-filter': enhanced && map.tiles.length ? `url(#${id})` : 'none' } as CSSProperties}>
    {enhanced && map.tiles.length > 0 && <svg className="optics-definitions" aria-hidden="true"><defs><filter id={id} x="0" y="0" width={map.width} height={map.height} filterUnits="userSpaceOnUse" colorInterpolationFilters="sRGB">{map.tiles.map((href, index) => <feImage key={index} href={href} x={xs[index % 3]} y={ys[Math.floor(index / 3)]} width={widths[index % 3]} height={heights[Math.floor(index / 3)]} preserveAspectRatio="none" result={`tile-${index}`}/>)}<feMerge result="normals">{map.tiles.map((_, index) => <feMergeNode key={index} in={`tile-${index}`}/>)}</feMerge><feGaussianBlur in="normals" stdDeviation=".75" result="smoothed"/><feComponentTransfer in="smoothed" result="lens"><feFuncR type="linear" intercept={-1 / 510}/><feFuncG type="linear" intercept={-1 / 510}/></feComponentTransfer><feDisplacementMap in="SourceGraphic" in2="lens" scale={DISPLACEMENT_SCALE * corner / TILE} xChannelSelector="R" yChannelSelector="G" result="refracted"/><feGaussianBlur in="refracted" stdDeviation={blur}/></filter></defs></svg>}
    <div className="glass-optics" aria-hidden="true"/>
    <div className="glass-refraction" aria-hidden="true"/>
    <div className="glass-surface" aria-hidden="true"/>
    <div className="glass-content">{children}</div>
  </div>;
}
