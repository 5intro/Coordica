import { act, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { GlassDock, type DockItem } from './GlassDock';

afterEach(() => vi.unstubAllGlobals());

it('keeps every compact navigation target steady during keyboard focus and restores desktop magnification', () => {
  let compact = true;
  vi.stubGlobal('matchMedia', vi.fn(() => ({ matches: compact })));
  const frames: FrameRequestCallback[] = [];
  vi.spyOn(window, 'requestAnimationFrame').mockImplementation((callback) => { frames.push(callback); return frames.length; });
  const items: DockItem[] = Array.from({ length: 9 }, (_, index) => ({ id: String(index), name: `页面 ${index}`, icon: 'home' }));
  const select = vi.fn();
  render(<GlassDock active="0" onSelect={select} appearance={{ motion: true, refraction: false }} items={items} />);
  const buttons = screen.getAllByRole('button');
  expect(buttons).toHaveLength(9);
  buttons[0].focus();
  fireEvent.keyDown(buttons[0], { key: 'End' });
  act(() => frames.splice(0).forEach((frame) => frame(0)));
  expect(document.activeElement).toBe(buttons[8]);
  expect(buttons[8].style.getPropertyValue('--zoom')).toBe('1');
  fireEvent.click(buttons[8]);
  expect(select).toHaveBeenLastCalledWith('8');
  compact = false;
  fireEvent.focus(buttons[0]);
  act(() => frames.splice(0).forEach((frame) => frame(0)));
  expect(Number(buttons[0].style.getPropertyValue('--zoom'))).toBeGreaterThan(1);
});
