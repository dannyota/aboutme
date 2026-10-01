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

/**
 * The element that holds the resume's box: it caps the measure and centers
 * itself (ResumeDocument.vue). The page root spans the viewport, so its own
 * right edge never leaves a margin.
 */
export const JOIN_INVITE_MEASURE_SELECTOR = '.public-measure';

/**
 * The bar's 56 px content band plus the bottom safe area; the page adds
 * bottom padding of this same total while the bar shows.
 */
export const JOIN_INVITE_BAR_BAND = '56px';
export const JOIN_INVITE_BAR_HEIGHT
  = `calc(${JOIN_INVITE_BAR_BAND} + env(safe-area-inset-bottom))`;

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
    // A resume shorter than the viewport has nothing to scroll: the scroll
    // criterion never fires, and only the 20 s dwell timer can show the
    // invite, rather than treating an unscrollable page as fully scrolled.
    if (scrollable <= 0) return 0;
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
 * margin beside the resume; every other viewport gets the bottom bar. The
 * margin is measured from the resume's own box inside `root`, and from
 * `root` itself when it holds none.
 */
export function joinInvitePlacement(
  root: Element,
  window_: Window = window,
): JoinInvitePlacement {
  if (window_.innerWidth < CARD_MIN_VIEWPORT_WIDTH) return 'bar';
  const measure = root.querySelector(JOIN_INVITE_MEASURE_SELECTOR) ?? root;
  const rect = measure.getBoundingClientRect();
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

/** Below this viewport width the bar drops its body text. */
export const JOIN_INVITE_BAR_TEXT_MIN_WIDTH = 360;

export const JOIN_INVITE_ENTRANCE = {
  keyframes: [
    { opacity: 0, transform: 'translateY(8px)' },
    { opacity: 1, transform: 'translateY(0)' },
  ],
  options: { duration: 200, easing: 'ease-out' },
} as const;

// The public page bar's Button fill, Button border, Credit text, and Button
// shadow (docs/design/public-page-theme.md#tokens). The invite follows the
// page's scheme with these, so a light card never sits on a dark page.
interface JoinInvitePalette {
  readonly surface: string;
  readonly border: string;
  readonly text: string;
  readonly shadow: string;
}

const LIGHT_PALETTE: JoinInvitePalette = {
  surface: '#FFFFFF',
  border: '#E5E1D6',
  text: '#5C6178',
  shadow: '0 1px 2px rgba(16, 27, 63, 0.06)',
};

const DARK_PALETTE: JoinInvitePalette = {
  surface: '#141A2E',
  border: 'rgba(230, 225, 210, 0.13)',
  text: '#A5ABBF',
  shadow: 'none',
};

export const JOIN_INVITE_TEXT_COLOR = LIGHT_PALETTE.text;

export const joinInvitePalette = (dark: boolean): JoinInvitePalette =>
  dark ? DARK_PALETTE : LIGHT_PALETTE;

/**
 * Whether the public page the invite sits on shows its dark roles: the
 * page's `data-color-scheme` is `dark`, or `system` while the viewer prefers
 * dark. Reads the viewer's preference in the browser only; nothing leaves it.
 */
export function joinInviteIsDark(
  root: Element,
  window_: Window = window,
): boolean {
  const scheme = root.querySelector('.public-resume-page')
    ?.getAttribute('data-color-scheme');
  if (scheme === 'dark') return true;
  return scheme === 'system'
    && window_.matchMedia?.('(prefers-color-scheme: dark)').matches === true;
}

/** Inline geometry: the public page loads no stylesheet for the invite. */
export function joinInviteContainerStyle(
  placement: JoinInvitePlacement,
  dark = false,
): Record<string, string | number> {
  const palette = joinInvitePalette(dark);
  const shared = {
    background: palette.surface,
    border: `1px solid ${palette.border}`,
    color: palette.text,
    fontFamily: '"Be Vietnam Pro", Inter, system-ui, sans-serif',
    position: 'fixed',
    zIndex: 40,
  };
  if (placement === 'card') {
    return {
      ...shared,
      right: '16px',
      bottom: '16px',
      boxSizing: 'border-box',
      width: '320px',
      borderRadius: '14px',
      padding: '16px',
      boxShadow: palette.shadow,
    };
  }
  return {
    ...shared,
    left: '0',
    right: '0',
    bottom: '0',
    boxSizing: 'border-box',
    height: JOIN_INVITE_BAR_HEIGHT,
    display: 'flex',
    alignItems: 'center',
    gap: '8px',
    border: 'none',
    borderTop: `1px solid ${palette.border}`,
    padding:
      '0 calc(12px + env(safe-area-inset-right)) '
      + 'env(safe-area-inset-bottom) calc(12px + env(safe-area-inset-left))',
  };
}
