import { computed, onBeforeUnmount, onMounted, ref, shallowRef } from 'vue';

import {
  decodeDeploymentDocument,
  deploymentDocumentPath,
  type FetchOutcome,
  maxDocumentBytes,
  pageState,
  responseTime,
} from '@/utils/deploymentDocument';

/** Refetch cadence while the tab is visible (page.md, behavior). */
export const refreshIntervalMs = 60_000;
/** How often ages and staleness are re-read between fetches. */
const tickMs = 10_000;

interface Clock {
  /** The server's time at the response, or null without a usable `Date`. */
  readonly serverTime: number | null;
  /** `performance.now()` when the response arrived. */
  readonly localTime: number;
}

async function readDocument(): Promise<{
  outcome: FetchOutcome;
  clock: Clock;
} | null> {
  let response: Response;
  try {
    response = await fetch(deploymentDocumentPath, {
      cache: 'no-cache',
      credentials: 'omit',
      redirect: 'error',
      headers: { accept: 'application/json' },
    });
  } catch {
    return null;
  }
  const clock = {
    serverTime: responseTime(
      response.headers.get('date'),
      response.headers.get('age'),
    ),
    localTime: performance.now(),
  };
  if (!response.ok) return { outcome: { kind: 'failed' }, clock };
  let body: unknown;
  try {
    const text = await response.text();
    if (new TextEncoder().encode(text).byteLength > maxDocumentBytes) {
      return { outcome: { kind: 'failed' }, clock };
    }
    body = JSON.parse(text);
  } catch {
    return { outcome: { kind: 'failed' }, clock };
  }
  return {
    outcome: { kind: 'decoded', result: decodeDeploymentDocument(body) },
    clock,
  };
}

/**
 * Fetches the same-origin deployment document in the browser only, refetches
 * it every minute while the tab is visible, and keeps a clock anchored to the
 * response's `Date` and `Age` so a wrong visitor clock cannot hide staleness.
 * A failed refetch keeps the last document, which then ages into stale.
 */
export function useDeploymentDocument() {
  const outcome = shallowRef<FetchOutcome>({ kind: 'pending' });
  const clock = shallowRef<Clock | null>(null);
  const tick = ref(0);
  let refreshTimer: ReturnType<typeof setInterval> | undefined;
  let tickTimer: ReturnType<typeof setInterval> | undefined;
  let lastFetch = -Infinity;
  let inFlight = false;

  async function refresh(): Promise<void> {
    if (inFlight) return;
    inFlight = true;
    lastFetch = performance.now();
    try {
      const read = await readDocument();
      const hadDocument = outcome.value.kind === 'decoded'
        && outcome.value.result.kind === 'document';
      if (read === null || (read.outcome.kind === 'failed' && hadDocument)) {
        if (!hadDocument) outcome.value = { kind: 'failed' };
        return;
      }
      outcome.value = read.outcome;
      clock.value = read.clock;
    } finally {
      inFlight = false;
    }
  }

  function start(): void {
    stop();
    refreshTimer = setInterval(() => void refresh(), refreshIntervalMs);
    tickTimer = setInterval(() => {
      tick.value += 1;
    }, tickMs);
  }

  function stop(): void {
    clearInterval(refreshTimer);
    clearInterval(tickTimer);
    refreshTimer = undefined;
    tickTimer = undefined;
  }

  function onVisibility(): void {
    if (document.visibilityState !== 'visible') {
      stop();
      return;
    }
    if (performance.now() - lastFetch >= refreshIntervalMs) void refresh();
    start();
  }

  /** The server's current time, or null when the response gave none. */
  const now = computed<number | null>(() => {
    void tick.value;
    const anchor = clock.value;
    if (anchor === null || anchor.serverTime === null) return null;
    return anchor.serverTime + (performance.now() - anchor.localTime);
  });

  const state = computed(() => pageState(outcome.value, now.value));

  onMounted(() => {
    document.addEventListener('visibilitychange', onVisibility);
    void refresh();
    if (document.visibilityState === 'visible') start();
  });
  onBeforeUnmount(() => {
    document.removeEventListener('visibilitychange', onVisibility);
    stop();
  });

  return { state, now, refresh };
}
