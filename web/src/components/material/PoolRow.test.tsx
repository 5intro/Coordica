import { fireEvent, render, screen } from '@testing-library/react';
import { afterEach, expect, it } from 'vitest';
import { PoolRow } from './PoolRow';
const key = 'coordica.test.pool-height.0';
afterEach(() => localStorage.removeItem(key));
const view = () => <PoolRow row={0} count={7} storageKey="coordica.test.pool-height"><div className="pool-well"/><div className="pool-well"/></PoolRow>;
it('bounds keyboard resizing, restores the local preference, and supports an explicit reset', () => {
  const first = render(view());
  const grip = screen.getByRole('separator');
  expect(grip.getAttribute('aria-valuetext')).toBe('默认高度');
  fireEvent.keyDown(grip,{key:'End'});
  expect(grip.getAttribute('aria-valuenow')).toBe('560');
  fireEvent.keyDown(grip,{key:'ArrowDown'});
  expect(grip.getAttribute('aria-valuenow')).toBe('560');
  first.unmount();
  render(view());
  expect(screen.getByRole('separator').getAttribute('aria-valuenow')).toBe('560');
  for (let i=0;i<40;i++) fireEvent.keyDown(screen.getByRole('separator'),{key:'ArrowUp'});
  expect(screen.getByRole('separator').getAttribute('aria-valuenow')).toBe('112');
  fireEvent.keyDown(screen.getByRole('separator'),{key:'Home'});
  expect(screen.getByRole('separator').getAttribute('aria-valuetext')).toBe('默认高度');
  expect(localStorage.getItem(key)).toBeNull();
});
it('ignores a corrupt or out-of-range stored height instead of breaking the layout', () => {
  localStorage.setItem(key,'100000');
  render(view());
  expect(screen.getByRole('separator').getAttribute('aria-valuetext')).toBe('默认高度');
});
