// Sign-in-to-view join invite: timing, placement, and 90-day close
// persistence (docs/design/viewer-analytics/sign-in-to-view.md#join-invite).
// Runs only on a `sign_in` resume's page and never touches viewer data.

/** The invite never appears before this, however fast the reader scrolls. */
export const JOIN_INVITE_MIN_DELAY_MS = 5_000;
/** Or after this much visible time, whichever comes first. */
export const JOIN_INVITE_VISIBLE_MS = 20_000;
/** Or once the reader has scrolled this far down the page. */
export const JOIN_INVITE_SCROLL_FRACTION = 0.6;

export const JOIN_INVITE_STORAGE_KEY = 'aboutme.joinInvite.closedAt';
const JOIN_INVITE_CLOSED_MS = 90 * 24 * 60 * 60 * 1000;

/** Wide enough, with enough right margin, for the fixed card placement. */
const CARD_MIN_VIEWPORT_WIDTH = 1_024;
const CARD_MIN_RIGHT_MARGIN = 360;

export type JoinInvitePlacement = 'card' | 'bar';

export interface JoinInviteTimingOptions {
  readonly window?: Window;
  readonly document?: Document;
  readonly now?: () => number;
  readonly setTimeout?: typeof setTimeout;
  readonly clearTimeout?: typeof clearTimeout;
}

/**
 * Calls `onShow` once, the first time the timing condition is met, never
 * before the 5 s floor. Returns a function that stops watching; calling it
 * after `onShow` has already fired is a no-op.
 */
export function startJoinInviteTiming(
  onShow: () => void,
  options: JoinInviteTimingOptions = {},
): () => void {
  const win = options.window ?? window;
  const doc = options.document ?? document;
  const now = options.now ?? (() => Date.now());
  const setTimeoutFn = options.setTimeout ?? setTimeout;
  const clearTimeoutFn = options.clearTimeout ?? clearTimeout;

  let shown = false;
  let armed = false;
  let accumulatedVisibleMs = 0;
  let visibleSince: number | null = (
    doc.visibilityState === 'visible' ? now() : null
  );
  let minDelayTimer: ReturnType<typeof setTimeout> | null = null;
  let checkTimer: ReturnType<typeof setTimeout> | null = null;

  function currentVisibleMs(): number {
    const sinceVisible = visibleSince !== null ? now() - visibleSince : 0;
    return accumulatedVisibleMs + sinceVisible;
  }

  function scrollFraction(): number {
    const scrollable = doc.documentElement.scrollHeight - win.innerHeight;
    if (scrollable <= 0) return 1;
    return Math.min(1, Math.max(0, win.scrollY / scrollable));
  }

  function clearCheckTimer(): void {
    if (checkTimer !== null) {
      clearTimeoutFn(checkTimer);
      checkTimer = null;
    }
  }

  function scheduleCheck(): void {
    if (shown || !armed || checkTimer !== null || visibleSince === null) {
      return;
    }
    const remaining = JOIN_INVITE_VISIBLE_MS - currentVisibleMs();
    if (remaining <= 0) {
      maybeShow();
      return;
    }
    checkTimer = setTimeoutFn(() => {
      checkTimer = null;
      maybeShow();
    }, remaining);
  }

  function maybeShow(): void {
    if (shown || !armed) return;
    if (
      scrollFraction() >= JOIN_INVITE_SCROLL_FRACTION
      || currentVisibleMs() >= JOIN_INVITE_VISIBLE_MS
    ) {
      shown = true;
      stop();
      onShow();
    }
  }

  function onScroll(): void {
    maybeShow();
  }

  function onVisibilityChange(): void {
    if (doc.visibilityState === 'visible') {
      if (visibleSince === null) visibleSince = now();
      scheduleCheck();
      return;
    }
    if (visibleSince !== null) {
      accumulatedVisibleMs += now() - visibleSince;
      visibleSince = null;
    }
    clearCheckTimer();
  }

  function stop(): void {
    if (minDelayTimer !== null) {
      clearTimeoutFn(minDelayTimer);
      minDelayTimer = null;
    }
    clearCheckTimer();
    win.removeEventListener('scroll', onScroll);
    doc.removeEventListener('visibilitychange', onVisibilityChange);
  }

  win.addEventListener('scroll', onScroll, { passive: true });
  doc.addEventListener('visibilitychange', onVisibilityChange);
  minDelayTimer = setTimeoutFn(() => {
    minDelayTimer = null;
    armed = true;
    maybeShow();
    scheduleCheck();
  }, JOIN_INVITE_MIN_DELAY_MS);

  return stop;
}

/**
 * The card placement needs a wide viewport and at least 360 px of right
 * margin beside the resume; every other viewport gets the bottom bar.
 */
export function joinInvitePlacement(
  root: Element,
  window_: Window = window,
): JoinInvitePlacement {
  if (window_.innerWidth < CARD_MIN_VIEWPORT_WIDTH) return 'bar';
  const rect = root.getBoundingClientRect();
  const rightMargin = window_.innerWidth - rect.right;
  return rightMargin >= CARD_MIN_RIGHT_MARGIN ? 'card' : 'bar';
}

/** Whether a close in this browser is still within its 90-day window. */
export function isJoinInviteClosed(
  storage: Pick<Storage, 'getItem'> = localStorage,
  now: () => number = Date.now,
): boolean {
  try {
    const value = storage.getItem(JOIN_INVITE_STORAGE_KEY);
    if (value === null) return false;
    const closedAt = Number(value);
    return Number.isFinite(closedAt)
      && now() - closedAt < JOIN_INVITE_CLOSED_MS;
  } catch {
    return false;
  }
}

/** Records a close (or following the link) for 90 days in this browser. */
export function recordJoinInviteClosed(
  storage: Pick<Storage, 'setItem'> = localStorage,
  now: () => number = Date.now,
): void {
  try {
    storage.setItem(JOIN_INVITE_STORAGE_KEY, String(now()));
  } catch {
    // Private browsing, a full quota, or storage disabled: never surface it.
  }
}
