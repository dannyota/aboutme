// @vitest-environment happy-dom

import { afterEach, describe, expect, it, vi } from 'vitest';

import { bindPageScroller, nearestPage } from '../../app/public/pageScroller';

const rect = (left: number, width: number): DOMRect => ({
  x: left, y: 0, width, height: 100, top: 0, right: left + width,
  bottom: 100, left, toJSON: () => ({}),
});

const scrollerWith = (count: number, scrollLeft: number) => {
  const scroller = document.createElement('div');
  scroller.getBoundingClientRect = () => rect(0, 390);
  Object.defineProperty(scroller, 'clientWidth', { value: 390 });
  for (let index = 0; index < count; index += 1) {
    const page = document.createElement('section');
    page.className = 'public-page';
    page.dataset.pageIndex = String(index);
    page.getBoundingClientRect = () => rect(index * 390 - scrollLeft, 390);
    Object.defineProperty(page, 'offsetLeft', { value: index * 390 });
    Object.defineProperty(page, 'offsetWidth', { value: 390 });
    scroller.append(page);
  }
  document.body.append(scroller);
  return scroller;
};

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  document.body.replaceChildren();
});

describe('page scroller', () => {
  it('finds the page nearest the scroller center', () => {
    expect(nearestPage(scrollerWith(3, 0))).toBe(0);
    document.body.replaceChildren();
    expect(nearestPage(scrollerWith(3, 500))).toBe(1);
    document.body.replaceChildren();
    expect(nearestPage(scrollerWith(3, 780))).toBe(2);
  });

  it('centers the target page, smoothly or as a jump', () => {
    const scroller = scrollerWith(3, 0);
    const scrollTo = vi.fn();
    scroller.scrollTo = scrollTo as typeof scroller.scrollTo;
    const bound = bindPageScroller(scroller, {
      onIndex: vi.fn(), onSettle: vi.fn(),
    });
    bound.scrollTo(2, false);
    bound.scrollTo(9, true);
    expect(scrollTo.mock.calls).toEqual([
      [{ left: 780, behavior: 'instant' }],
      [{ left: 780, behavior: 'smooth' }],
    ]);
    bound.dispose();
  });

  it('reports the settled page when a scroll ends', () => {
    const scroller = scrollerWith(3, 390);
    const onSettle = vi.fn();
    const bound = bindPageScroller(scroller, { onIndex: vi.fn(), onSettle });
    scroller.dispatchEvent(new Event('scrollend'));
    expect(onSettle).toHaveBeenCalledWith(1);
    bound.dispose();
  });

  it('turns snapping off while the reader zooms in', () => {
    const viewport = Object.assign(new EventTarget(), { scale: 2 });
    vi.stubGlobal('visualViewport', viewport);
    const scroller = scrollerWith(2, 0);
    const bound = bindPageScroller(scroller, {
      onIndex: vi.fn(), onSettle: vi.fn(),
    });
    expect(scroller.hasAttribute('data-zoomed')).toBe(true);
    viewport.scale = 1;
    viewport.dispatchEvent(new Event('resize'));
    expect(scroller.hasAttribute('data-zoomed')).toBe(false);
    bound.dispose();
  });
});
