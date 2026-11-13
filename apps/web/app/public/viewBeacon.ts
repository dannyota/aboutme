import { solveChallenge } from 'altcha-lib';
import { deriveKey } from 'altcha-lib/algorithms/web/pbkdf2';
import type { components } from '../api/generated/openapi';

type ViewChallenge = components['schemas']['ViewChallenge'];

interface ViewStartData {
  readonly owner: boolean;
  readonly signedIn: boolean;
  readonly token?: string;
  readonly challenge?: ViewChallenge;
}

const VISIBLE_MS_REQUIRED = 8_000;
const SOLVE_TIMEOUT_MS = 60_000;
const TOKEN_LIFETIME_MS = 29 * 60_000;

const INTERACTION_EVENTS = [
  'scroll', 'pointerdown', 'touchstart', 'keydown',
] as const;

export interface ViewBeaconOptions {
  readonly slug: string;
  readonly fetch?: typeof fetch;
  readonly document?: Document;
  readonly target?: EventTarget;
  readonly solve?: typeof solveChallenge;
  readonly now?: () => number;
  readonly setTimeout?: typeof setTimeout;
  readonly clearTimeout?: typeof clearTimeout;
  readonly onStart?: (result: {
    readonly owner: boolean;
    readonly signedIn: boolean;
  }) => void;
}

export function startViewBeacon(options: ViewBeaconOptions): void {
  void runViewBeacon(options).catch(() => {
  });
}

async function runViewBeacon(options: ViewBeaconOptions): Promise<void> {
  const fetchFn = options.fetch ?? globalThis.fetch;
  const doc = options.document ?? document;
  const target = options.target ?? window;
  const solveFn = options.solve ?? solveChallenge;
  const now = options.now ?? (() => Date.now());
  const setTimeoutFn = options.setTimeout ?? setTimeout;
  const clearTimeoutFn = options.clearTimeout ?? clearTimeout;

  let response: Response;
  try {
    response = await fetchFn(
      `/api/v1/public/resumes/${encodeURIComponent(options.slug)}/views/start`,
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'same-origin',
        cache: 'no-store',
        body: '{}',
      },
    );
  } catch {
    return;
  }
  if (!response.ok) return;

  let data: ViewStartData;
  try {
    const body = (await response.json()) as { data?: ViewStartData };
    if (!body.data) return;
    data = body.data;
  } catch {
    return;
  }
  options.onStart?.({ owner: data.owner, signedIn: data.signedIn === true });
  if (data.owner || !data.token || !data.challenge) return;
  const token = data.token;
  const challenge = data.challenge;

  let sent = false;
  let interacted = false;
  let solution: { counter: number; derivedKey: string } | null = null;
  let accumulatedVisibleMs = 0;
  let visibleSince: number | null = (
    doc.visibilityState === 'visible' ? now() : null
  );
  let checkTimer: ReturnType<typeof setTimeout> | null = null;
  let expiryTimer: ReturnType<typeof setTimeout> | null = null;

  function currentVisibleMs(): number {
    const sinceVisible = visibleSince !== null ? now() - visibleSince : 0;
    return accumulatedVisibleMs + sinceVisible;
  }

  function clearCheckTimer(): void {
    if (checkTimer !== null) {
      clearTimeoutFn(checkTimer);
      checkTimer = null;
    }
  }

  function scheduleCheck(): void {
    if (sent || checkTimer !== null || visibleSince === null) return;
    const remaining = VISIBLE_MS_REQUIRED - currentVisibleMs();
    if (remaining <= 0) {
      maybeSend();
      return;
    }
    checkTimer = setTimeoutFn(() => {
      checkTimer = null;
      maybeSend();
    }, remaining);
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

  function onInteraction(event: Event): void {
    if (!event.isTrusted || interacted) return;
    interacted = true;
    removeInteractionListeners();
    maybeSend();
  }

  function addInteractionListeners(): void {
    for (const type of INTERACTION_EVENTS) {
      target.addEventListener(type, onInteraction, { passive: true });
    }
  }

  function removeInteractionListeners(): void {
    for (const type of INTERACTION_EVENTS) {
      target.removeEventListener(type, onInteraction);
    }
  }

  function cleanup(): void {
    clearCheckTimer();
    if (expiryTimer !== null) {
      clearTimeoutFn(expiryTimer);
      expiryTimer = null;
    }
    removeInteractionListeners();
    doc.removeEventListener('visibilitychange', onVisibilityChange);
  }

  function maybeSend(): void {
    if (sent) return;
    if (currentVisibleMs() < VISIBLE_MS_REQUIRED) return;
    if (!interacted || solution === null) return;
    sent = true;
    cleanup();
    void sendCollect();
  }

  async function sendCollect(): Promise<void> {
    if (solution === null) return;
    try {
      await fetchFn('/api/v1/public/views/collect', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'same-origin',
        keepalive: true,
        body: JSON.stringify({
          token,
          challenge,
          solution: {
            counter: solution.counter,
            derivedKey: solution.derivedKey,
          },
        }),
      });
    } catch {
      // Never retry; the response is ignored either way.
    }
  }

  doc.addEventListener('visibilitychange', onVisibilityChange);
  addInteractionListeners();
  scheduleCheck();
  expiryTimer = setTimeoutFn(() => {
    if (!sent) cleanup();
    sent = true;
  }, TOKEN_LIFETIME_MS);

  type LibChallenge = Parameters<typeof solveChallenge>[0]['challenge'];
  let solved: { counter: number; derivedKey: string } | null;
  try {
    solved = await solveFn({
      challenge: challenge as unknown as LibChallenge,
      deriveKey,
      timeout: SOLVE_TIMEOUT_MS,
    });
  } catch {
    solved = null;
  }
  if (sent) return;
  if (solved === null) {
    cleanup();
    sent = true;
    return;
  }
  solution = solved;
  maybeSend();
}
