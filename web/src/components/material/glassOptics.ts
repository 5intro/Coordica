/** Inward edge sampling keeps the source coordinate monotonic. A nearly flat
 * derivative at the rim slows the background there without folding it into bands. */
export const DISPLACEMENT_SCALE = 48;
export function edgeDisplacement(edge: number, bezel: number): number {
  const t = Math.max(0, Math.min(1, edge / bezel));
  return bezel * .96 * (.5 - t + t ** 3 - .5 * t ** 4);
}
export function lensOffset(x: number, y: number, width: number, height: number, radius: number) {
  const r = Math.max(1, Math.min(radius, width / 2, height / 2));
  const bezel = Math.min(22, r, height / 3);
  const qx = x - Math.max(r, Math.min(width - r, x));
  const qy = y - Math.max(r, Math.min(height - r, y));
  const distance = Math.hypot(qx, qy);
  let nx = 0, ny = 0, edge: number;
  if (distance > 0) {
    nx = qx / distance; ny = qy / distance; edge = r - distance;
  } else {
    edge = Math.min(x, width - x, y, height - y);
    if (edge === x) nx = -1;
    else if (edge === width - x) nx = 1;
    else if (edge === y) ny = -1;
    else ny = 1;
  }
  // Extend the same field outside the rounded clip; resetting it there creates a seam.
  const displacement = edgeDisplacement(edge, bezel);
  return { x: -nx * displacement, y: -ny * displacement };
}
