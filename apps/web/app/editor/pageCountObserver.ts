export interface PageCountObserverDeps {
  requestAnimationFrame(callback: FrameRequestCallback): number;
  cancelAnimationFrame(handle: number): void;
  createMutationObserver(
    callback: MutationCallback,
  ): Pick<MutationObserver, 'observe' | 'disconnect'>;
  isVisible(page: HTMLElement): boolean;
}

const selector = [
  '.resume-page[data-page-index]',
  ':not(.pagination-measurement)',
  ':not([aria-hidden="true"])',
  ':not([hidden])',
  ':not([inert])',
].join('');

function browserDeps(): PageCountObserverDeps {
  return {
    requestAnimationFrame: (callback) => window.requestAnimationFrame(callback),
    cancelAnimationFrame: (handle) => window.cancelAnimationFrame(handle),
    createMutationObserver: (callback) => new MutationObserver(callback),
    isVisible: (page) => {
      const style = window.getComputedStyle(page);
      return style.display !== 'none' && style.visibility !== 'hidden';
    },
  };
}

export function observeSettledVisiblePageCount(
  root: HTMLElement,
  onSettled: (count: number) => void,
  deps: PageCountObserverDeps = browserDeps(),
): () => void {
  let frame = 0;
  let first: string | null = null;
  let stopped = false;

  const readSignature = (): string | null => {
    const indexes = [...root.querySelectorAll<HTMLElement>(selector)]
      .filter((page) => deps.isVisible(page))
      .map((page) => page.dataset.pageIndex ?? '');
    if (
      indexes.length === 0
      || indexes.some((value, index) => value !== String(index))
    ) {
      return null;
    }
    return indexes.join(',');
  };
  const cancel = (): void => {
    if (frame !== 0) deps.cancelAnimationFrame(frame);
    frame = 0;
    first = null;
  };
  const schedule = (): void => {
    cancel();
    if (stopped) return;
    frame = deps.requestAnimationFrame(() => {
      frame = 0;
      first = readSignature();
      if (first === null || stopped) return;
      frame = deps.requestAnimationFrame(() => {
        frame = 0;
        if (stopped) return;
        const second = readSignature();
        if (second === first && second !== null) {
          onSettled(second.split(',').length);
        }
      });
    });
  };
  const observer = deps.createMutationObserver(schedule);
  observer.observe(root, {
    subtree: true,
    childList: true,
    attributes: true,
    attributeFilter: [
      'class',
      'data-page-index',
      'hidden',
      'inert',
      'aria-hidden',
    ],
  });
  schedule();

  return () => {
    stopped = true;
    observer.disconnect();
    cancel();
  };
}
