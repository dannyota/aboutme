export const PAGES_TIMEOUT_MS = 3_000;
export const PHONE_QUERY = '(width < 600px)';
export const PAGE_CHANGE_EVENT = 'public-page-change';
const SHEET_GUTTERS_PX = 32;

export type PagesState
  = | 'loading'
    | 'staging'
    | 'shown'
    | 'updating'
    | 'continuous';

export interface PagesTimers {
  setTimeout(callback: () => void, ms: number): number;
  clearTimeout(id: number): void;
  requestAnimationFrame(callback: FrameRequestCallback): number;
  cancelAnimationFrame(id: number): void;
}

export interface PagesVisibility {
  hidden(): boolean;
  subscribe(listener: () => void): () => void;
}

export interface PagesControllerOptions {
  readonly withinFirstPage: () => boolean;
  readonly onState: (state: PagesState) => void;
  readonly onPages?: (count: number, update: boolean) => void;
  readonly timeoutMs?: number;
  readonly timers?: PagesTimers;
  readonly visibility?: PagesVisibility;
}

export interface PagesController {
  state(): PagesState;
  load(fonts: Promise<unknown>): void;
  settled(count: number): void;
  failed(): void;
  dispose(): void;
}

export function pageScale(clientWidth: number, pageWidthPx: number): number {
  if (!Number.isFinite(clientWidth) || !(pageWidthPx > 0)) return 1;
  return Math.min(1, Math.max(0, (clientWidth - SHEET_GUTTERS_PX)
    / pageWidthPx));
}

export function clampPageIndex(index: number, count: number): number {
  if (count <= 0) return 0;
  return Math.min(Math.max(0, Math.trunc(index)), count - 1);
}

export function pageProgress(index: number, count: number): number {
  if (count <= 0) return 0;
  return (clampPageIndex(index, count) + 1) / count;
}

const browserTimers = (): PagesTimers => ({
  setTimeout: (callback, ms) => window.setTimeout(callback, ms),
  clearTimeout: (id) => window.clearTimeout(id),
  requestAnimationFrame: (callback) => window.requestAnimationFrame(callback),
  cancelAnimationFrame: (id) => window.cancelAnimationFrame(id),
});

const browserVisibility = (): PagesVisibility => ({
  hidden: () => window.document.visibilityState === 'hidden',
  subscribe: (listener) => {
    window.document.addEventListener('visibilitychange', listener);
    return () => window.document.removeEventListener(
      'visibilitychange',
      listener,
    );
  },
});

export function createPagesController(
  options: PagesControllerOptions,
): PagesController {
  const timers = options.timers ?? browserTimers();
  const visibility = options.visibility ?? browserVisibility();
  const timeoutMs = options.timeoutMs ?? PAGES_TIMEOUT_MS;
  let state: PagesState = 'loading';
  let attempt = 0;
  let settledAttempt = -1;
  let timer: number | undefined;
  let frame: number | undefined;
  let disposed = false;
  let waiting = false;

  const setState = (next: PagesState): void => {
    if (state === next) return;
    state = next;
    options.onState(next);
  };
  const clearTimer = (): void => {
    if (timer !== undefined) timers.clearTimeout(timer);
    timer = undefined;
    waiting = false;
  };
  const clearFrame = (): void => {
    if (frame !== undefined) timers.cancelAnimationFrame(frame);
    frame = undefined;
  };
  const failed = (): void => {
    if (disposed || state === 'continuous') return;
    clearTimer();
    clearFrame();
    setState('continuous');
  };
  const schedule = (): void => {
    timer = timers.setTimeout(() => {
      timer = undefined;
      waiting = false;
      failed();
    }, timeoutMs);
  };
  const startTimer = (): void => {
    clearTimer();
    waiting = true;
    if (!visibility.hidden()) schedule();
  };
  const unsubscribe = visibility.subscribe(() => {
    if (disposed || !waiting) return;
    if (timer !== undefined) {
      timers.clearTimeout(timer);
      timer = undefined;
    }
    if (!visibility.hidden()) schedule();
  });
  const arm = (current: number): void => {
    if (disposed || current !== attempt || settledAttempt === current) return;
    if (state === 'loading') setState('staging');
    if (state === 'staging') startTimer();
  };

  return {
    state: () => state,
    load: (fonts) => {
      if (disposed) return;
      attempt += 1;
      const current = attempt;
      clearTimer();
      clearFrame();
      if (state === 'shown' || state === 'updating') {
        setState('updating');
        startTimer();
      } else if (state === 'continuous') setState('loading');
      fonts.then(() => arm(current), () => {
        if (current === attempt) failed();
      });
    },
    settled: (count) => {
      if (disposed) return;
      if (state === 'shown' || state === 'updating') {
        const update = state === 'updating';
        settledAttempt = attempt;
        clearTimer();
        setState('shown');
        options.onPages?.(count, update);
        return;
      }
      if (state !== 'staging') return;
      settledAttempt = attempt;
      clearTimer();
      clearFrame();
      frame = timers.requestAnimationFrame(() => {
        frame = undefined;
        if (disposed || state !== 'staging') return;
        if (!options.withinFirstPage()) {
          setState('continuous');
          return;
        }
        setState('shown');
        options.onPages?.(count, false);
      });
    },
    failed,
    dispose: () => {
      disposed = true;
      unsubscribe();
      clearTimer();
      clearFrame();
    },
  };
}
