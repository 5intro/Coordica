import { act, render } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { Glass } from './Glass';

afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); });
it('shares bounded refraction bitmaps across many cards and repeated resizing', () => {
  vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue('Chrome/150.0');
  const observers: ResizeObserverCallback[] = [];
  vi.stubGlobal('ResizeObserver', class { constructor(callback: ResizeObserverCallback) { observers.push(callback); } observe() {} disconnect() {} });
  const sizes: number[][] = [];
  vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockImplementation(() => ({
    createImageData(width: number, height: number) { sizes.push([width, height]); return { data: new Uint8ClampedArray(width * height * 4) }; },
    putImageData() {},
  }) as unknown as CanvasRenderingContext2D);
  const encode = vi.spyOn(HTMLCanvasElement.prototype, 'toDataURL').mockImplementation(() => `data:image/png;base64,${sizes.length}`);
  const view = render(<>{Array.from({ length: 64 }, (_, i) => <Glass key={i}>Card {i}</Glass>)}</>);
  function resize(delta: number) { act(() => observers.forEach((callback, i) => callback([{ contentRect: { width: 300 + i + delta, height: 100 + i } } as ResizeObserverEntry], {} as ResizeObserver))); }
  resize(0); resize(1000); resize(10);
  expect(encode).toHaveBeenCalledTimes(9);
  expect(sizes).toEqual(Array.from({ length: 9 }, () => [32, 32]));
  const filters = Array.from(view.container.querySelectorAll('filter'));
  expect(filters).toHaveLength(64);
  expect(new Set(Array.from(view.container.querySelectorAll('feImage')).map((image) => image.getAttribute('href'))).size).toBe(9);
  expect(filters[0].getAttribute('width')).toBe('310');
  expect(filters[0].querySelectorAll('feMergeNode')).toHaveLength(9);
});
