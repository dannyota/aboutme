import { computed, onBeforeUnmount, onMounted, ref, shallowRef } from 'vue';

import {
  decodeDeploymentDocument,
  deploymentDocumentPath,
  type FetchOutcome,
  maxDocumentBytes,
  pageState,
  responseTime,
} from '@/utils/deploymentDocument';

const refreshIntervalMs = 60_000;
const tickMs = 10_000;

interface Clock {
  readonly serverTime: number | null;
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
