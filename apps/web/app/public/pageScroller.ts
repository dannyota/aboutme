import { clampPageIndex } from './publicPages';

const SHEET_SELECTOR = '.public-page';
const SETTLE_FALLBACK_MS = 150;

export interface PageScrollerOptions {
  readonly onIndex: (index: number) => void;
  readonly onSettle: (index: number) => void;
}

export interface PageScroller {
  observe(): void;
  scrollTo(index: number, smooth: boolean): void;
  dispose(): void;
}

const supportsScrollEnd = (target: object): boolean =>
  'onscrollend' in target;

const sheets = (scroller: HTMLElement): HTMLElement[] =>
  [...scroller.querySelectorAll<HTMLElement>(`:scope > ${SHEET_SELECTOR}`)];

export function nearestPage(scroller: HTMLElement): number {
  const pages = sheets(scroller);
  const box = scroller.getBoundingClientRect();
  const center = box.left + box.width / 2;
  let best = 0;
  let distance = Number.POSITIVE_INFINITY;
  for (const [index, page] of pages.entries()) {
    const rect = page.getBoundingClientRect();
    const gap = Math.abs(rect.left + rect.width / 2 - center);
    if (gap < distance) {
      distance = gap;
      best = index;
    }
  }
  return best;
}

export function bindPageScroller(
  scroller: HTMLElement,
  options: PageScrollerOptions,
): PageScroller {
  const view = scroller.ownerDocument.defaultView ?? window;
  let observer: IntersectionObserver | undefined;
  let settleTimer: number | undefined;
  const nativeScrollEnd = supportsScrollEnd(view);

  const settle = (): void => options.onSettle(nearestPage(scroller));
  const onScroll = (): void => {
    if (nativeScrollEnd) return;
    if (settleTimer !== undefined) view.clearTimeout(settleTimer);
    settleTimer = view.setTimeout(() => {
      settleTimer = undefined;
      settle();
    }, SETTLE_FALLBACK_MS);
  };
  const zoom = (): void => {
    const zoomed = (view.visualViewport?.scale ?? 1) > 1;
    scroller.toggleAttribute('data-zoomed', zoomed);
  };

  scroller.addEventListener('scroll', onScroll, { passive: true });
  scroller.addEventListener('scrollend', settle);
  view.visualViewport?.addEventListener('resize', zoom);
  zoom();

  return {
    observe: () => {
      observer?.disconnect();
      if (typeof IntersectionObserver === 'undefined') return;
      observer = new IntersectionObserver((entries) => {
        for (const entry of entries) {
          if (!entry.isIntersecting || entry.intersectionRatio < 0.5) continue;
          const index = Number(
            (entry.target as HTMLElement).dataset.pageIndex,
          );
          if (Number.isInteger(index)) options.onIndex(index);
        }
      }, { root: scroller, threshold: [0.5] });
      for (const page of sheets(scroller)) observer.observe(page);
    },
    scrollTo: (index, smooth) => {
      const pages = sheets(scroller);
      const page = pages[clampPageIndex(index, pages.length)];
      if (page === undefined) return;
      const left = page.offsetLeft + page.offsetWidth / 2
        - scroller.clientWidth / 2;
      scroller.scrollTo({ left, behavior: smooth ? 'smooth' : 'instant' });
    },
    dispose: () => {
      observer?.disconnect();
      if (settleTimer !== undefined) view.clearTimeout(settleTimer);
      scroller.removeEventListener('scroll', onScroll);
      scroller.removeEventListener('scrollend', settle);
      view.visualViewport?.removeEventListener('resize', zoom);
    },
  };
}
