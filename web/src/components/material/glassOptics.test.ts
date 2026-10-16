import { expect, it } from 'vitest';
import { edgeDisplacement, lensOffset } from './glassOptics';

it('slows rim sampling without reversing its direction or creating a boundary jump', () => {
  const bezel = 22;
  let previous = edgeDisplacement(0, bezel);
  for (let edge = .01; edge <= bezel + 1; edge += .01) {
    const source = edge + edgeDisplacement(edge, bezel);
    expect(source).toBeGreaterThan(previous);
    previous = source;
  }
  const rimSpeed = (.01 + edgeDisplacement(.01, bezel) - edgeDisplacement(0, bezel)) / .01;
  expect(rimSpeed).toBeGreaterThan(0);
  expect(rimSpeed).toBeLessThan(.05);
  expect(edgeDisplacement(bezel, bezel)).toBe(0);
  expect(edgeDisplacement(bezel - .01, bezel)).toBeLessThan(.00001);
});
it('extends the rounded edge field smoothly and samples inward at each edge', () => {
  const args = [200, 100, 20] as const;
  expect(lensOffset(0, 50, ...args).x).toBeGreaterThan(0);
  expect(lensOffset(200, 50, ...args).x).toBeLessThan(0);
  expect(lensOffset(100, 0, ...args).y).toBeGreaterThan(0);
  expect(lensOffset(100, 100, ...args).y).toBeLessThan(0);
  expect(Math.hypot(...Object.values(lensOffset(100, 50, ...args)))).toBe(0);
  const outside = lensOffset(0, 20, ...args);
  const inside = lensOffset(.001, 20, ...args);
  expect(Math.abs(outside.x - inside.x)).toBeLessThan(.002);
  expect(Math.abs(outside.y - inside.y)).toBeLessThan(.002);
});
