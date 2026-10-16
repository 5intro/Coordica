import { useId, useLayoutEffect, useRef, useState, type CSSProperties, type ReactNode } from 'react';
import { DISPLACEMENT_SCALE, lensOffset } from './glassOptics';

const lensMaps = new Map<string, string>();

/** The map encodes a rounded lens's edge normals in the red/green channels.
 * Only the backdrop is displaced; text and controls remain sharp. */
function lensMap(width: number, height: number, radius: number): string {
  const key = `${width}:${height}:${radius}`;
  const cached = lensMaps.get(key);
  if (cached) return cached;
  const canvas = document.createElement('canvas');
  canvas.width = width;
  canvas.height = height;
  const context = canvas.getContext('2d');
  if (!context) return '';
  const pixels = context.createImageData(width, height);
  for (let y = 0; y < height; y++) {
    for (let x = 0; x < width; x++) {
      const offset = lensOffset(x + .5, y + .5, width, height, radius);
      const index = (y * width + x) * 4;
      pixels.data[index] = 128 + offset.x / DISPLACEMENT_SCALE * 255;
      pixels.data[index + 1] = 128 + offset.y / DISPLACEMENT_SCALE * 255;
      pixels.data[index + 2] = 128;
      pixels.data[index + 3] = 255;
    }
  }
  context.putImageData(pixels, 0, 0);
  const result = canvas.toDataURL();
  if (lensMaps.size >= 24) lensMaps.delete(lensMaps.keys().next().value!);
  lensMaps.set(key, result);
  return result;
}

export function Glass({ children, className = '', radius = 28, blur = .65, refract = true, style }: { children: ReactNode; className?: string; radius?: number; blur?: number; refract?: boolean; style?: CSSProperties }) {
  const id = `glass-${useId().replace(/[^a-zA-Z0-9]/g, '')}`;
  const ref = useRef<HTMLDivElement>(null);
  const [map, setMap] = useState({ href: '', width: 1, height: 1 });
  // Parsing url() alone does not detect SVG backdrop rendering support in WebKit.
  // Keep the known enhanced engine explicit and give other engines real blur.
  const enhanced = refract && /(?:Chrome|Chromium|Edg)\//.test(navigator.userAgent);
  useLayoutEffect(() => {
    const element = ref.current;
    if (!element || !enhanced) return;
    const observer = new ResizeObserver(([entry]) => {
      const width = Math.max(1, Math.ceil(entry.contentRect.width));
      const height = Math.max(1, Math.ceil(entry.contentRect.height));
      setMap({ href: lensMap(width, height, radius), width, height });
    });
    observer.observe(element);
    return () => observer.disconnect();
  }, [radius, enhanced]);
  return <div ref={ref} className={`glass ${className}`} style={{ ...style, '--glass-radius': `${radius}px`, '--glass-filter': enhanced && map.href ? `url(#${id})` : 'none' } as CSSProperties}>
    {enhanced && map.href && <svg className="optics-definitions" aria-hidden="true"><defs><filter id={id} x="0" y="0" width={map.width} height={map.height} filterUnits="userSpaceOnUse" colorInterpolationFilters="sRGB"><feImage href={map.href} x="0" y="0" width={map.width} height={map.height} result="normals"/><feGaussianBlur in="normals" stdDeviation=".75" result="smoothed"/><feComponentTransfer in="smoothed" result="lens"><feFuncR type="linear" intercept={-1 / 510}/><feFuncG type="linear" intercept={-1 / 510}/></feComponentTransfer><feDisplacementMap in="SourceGraphic" in2="lens" scale={DISPLACEMENT_SCALE} xChannelSelector="R" yChannelSelector="G" result="refracted"/><feGaussianBlur in="refracted" stdDeviation={blur}/></filter></defs></svg>}
    <div className="glass-optics" aria-hidden="true"/>
    <div className="glass-refraction" aria-hidden="true"/>
    <div className="glass-surface" aria-hidden="true"/>
    <div className="glass-content">{children}</div>
  </div>;
}
