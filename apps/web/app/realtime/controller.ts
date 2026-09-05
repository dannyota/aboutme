export type RealtimeRefetchMode = 'unconditional' | 'conditional';

export type RealtimeReadResult
  = | { readonly kind: 'updated' }
    | { readonly kind: 'unchanged' }
    | { readonly kind: 'not-found' }
    | { readonly kind: 'failed' }
    | { readonly kind: 'unknown-version' };

export type RevisionDecision = 'accept' | 'ignore' | 'unknown';

export interface RealtimeEventSource {
  onopen: ((event: Event) => void) | null;
  onerror: ((event: Event) => void) | null;
  addEventListener(
    type: string,
    listener: (event: MessageEvent<string>) => void,
  ): void;
  removeEventListener(
    type: string,
    listener: (event: MessageEvent<string>) => void,
  ): void;
  close(): void;
}

interface TimerApi {
  setTimeout: (
    callback: () => void,
    delay: number,
  ) => ReturnType<typeof setTimeout>;
  clearTimeout: (timer: ReturnType<typeof setTimeout>) => void;
}

export interface RealtimeControllerOptions {
  url: string;
  eventSourceFactory?: (
    url: string,
    withCredentials: boolean,
  ) => RealtimeEventSource;
  withCredentials?: boolean;
  refetch: (mode: RealtimeRefetchMode) => Promise<RealtimeReadResult>;
  onRevision: (value: unknown) => RevisionDecision;
  onUnknownVersion: () => void;
  onNotFound?: () => void;
  now?: () => number;
  timers?: TimerApi;
  silentAfterMs?: number;
  pollEveryMs?: number;
  failedRetryMs?: number;
}

export interface RealtimeController {
  start(): void;
  stop(): void;
}

const defaultEventSourceFactory = (
  url: string,
  withCredentials: boolean,
): RealtimeEventSource => {
  if (typeof EventSource === 'undefined') {
    throw new Error('EventSource is unavailable');
  }
  return new EventSource(url, { withCredentials });
};

function parseFrame(event: MessageEvent<string>): unknown | undefined {
  try {
    return JSON.parse(event.data) as unknown;
  } catch {
    return undefined;
  }
}

function isVersionOne(value: unknown): boolean {
  return (
    typeof value === 'object'
    && value !== null
    && 'version' in value
    && value.version === 1
  );
}

export function createRealtimeController(
  options: RealtimeControllerOptions,
): RealtimeController {
  const now = options.now ?? (() => Date.now());
  const timers = options.timers ?? {
    setTimeout: (callback, delay) => setTimeout(callback, delay),
    clearTimeout: (timer) => clearTimeout(timer),
  };
  const silentAfterMs = options.silentAfterMs ?? 60_000;
  const pollEveryMs = options.pollEveryMs ?? 30_000;
  const failedRetryMs = options.failedRetryMs ?? 5_000;
  const eventSourceFactory
    = options.eventSourceFactory ?? defaultEventSourceFactory;

  let source: RealtimeEventSource | null = null;
  let running = false;
  let generation = 0;
  let reading = false;
  let queuedMode: RealtimeRefetchMode | null = null;
  let silentTimer: ReturnType<typeof setTimeout> | null = null;
  let pollTimer: ReturnType<typeof setTimeout> | null = null;
  let repairTimer: ReturnType<typeof setTimeout> | null = null;
  let lastFrameAt = 0;
  let consecutiveErrors = 0;
  let fallback = false;
  let reloadIssued = false;

  const clearSilentTimer = (): void => {
    if (silentTimer === null) return;
    timers.clearTimeout(silentTimer);
    silentTimer = null;
  };

  const clearPollTimer = (): void => {
    if (pollTimer === null) return;
    timers.clearTimeout(pollTimer);
    pollTimer = null;
  };

  const clearRepairTimer = (): void => {
    if (repairTimer === null) return;
    timers.clearTimeout(repairTimer);
    repairTimer = null;
  };

  const unknownVersion = (): void => {
    if (reloadIssued) return;
    reloadIssued = true;
    options.onUnknownVersion();
  };

  const scheduleSilenceCheck = (): void => {
    clearSilentTimer();
    if (!running) return;
    silentTimer = timers.setTimeout(() => {
      silentTimer = null;
      if (!running) return;
      if (now() - lastFrameAt < silentAfterMs) {
        scheduleSilenceCheck();
        return;
      }
      fallback = true;
      schedulePoll();
    }, silentAfterMs);
  };

  const markFrame = (): void => {
    lastFrameAt = now();
    consecutiveErrors = 0;
    scheduleSilenceCheck();
  };

  const disableFallback = (): void => {
    fallback = false;
    clearPollTimer();
  };

  const handleReadResult = (result: RealtimeReadResult): void => {
    if (result.kind === 'not-found') options.onNotFound?.();
    if (result.kind === 'unknown-version') unknownVersion();
  };

  const scheduleRepairRetry = (): void => {
    if (!running || repairTimer !== null) return;
    repairTimer = timers.setTimeout(() => {
      repairTimer = null;
      scheduleRead('unconditional');
    }, failedRetryMs);
  };

  const runRead = (mode: RealtimeRefetchMode): void => {
    const readGeneration = generation;
    reading = true;
    void options
      .refetch(mode)
      .then((result) => {
        if (!running || readGeneration !== generation) return;
        handleReadResult(result);
        if (result.kind === 'failed') scheduleRepairRetry();
        else clearRepairTimer();
      })
      .catch(() => {
        if (running && readGeneration === generation) {
          scheduleRepairRetry();
        }
      })
      .finally(() => {
        if (!running || readGeneration !== generation) return;
        reading = false;
        const queued = queuedMode;
        queuedMode = null;
        if (queued !== null) runRead(queued);
      });
  };

  const scheduleRead = (mode: RealtimeRefetchMode): void => {
    if (!running) return;
    if (reading) {
      if (mode === 'unconditional' || queuedMode === null) queuedMode = mode;
      return;
    }
    runRead(mode);
  };

  function schedulePoll(): void {
    if (!running || !fallback || pollTimer !== null) return;
    pollTimer = timers.setTimeout(() => {
      pollTimer = null;
      if (!running || !fallback) return;
      scheduleRead('conditional');
      schedulePoll();
    }, pollEveryMs);
  }

  const onRevision = (event: MessageEvent<string>): void => {
    if (!running) return;
    markFrame();
    const value = parseFrame(event);
    if (value === undefined || !isVersionOne(value)) {
      unknownVersion();
      return;
    }
    const decision = options.onRevision(value);
    if (decision === 'unknown') unknownVersion();
    if (decision === 'accept') scheduleRead('unconditional');
  };

  const onHeartbeat = (event: MessageEvent<string>): void => {
    if (!running) return;
    const value = parseFrame(event);
    if (value === undefined || !isVersionOne(value)) {
      unknownVersion();
      return;
    }
    markFrame();
    disableFallback();
  };

  const onOpen = (): void => {
    if (!running) return;
    consecutiveErrors = 0;
    scheduleRead('unconditional');
  };

  const onError = (): void => {
    if (!running) return;
    consecutiveErrors += 1;
    scheduleRead('unconditional');
    if (consecutiveErrors >= 3) {
      fallback = true;
      schedulePoll();
    }
  };

  return {
    start: () => {
      if (running) return;
      running = true;
      generation += 1;
      const startGeneration = generation;
      lastFrameAt = now();
      consecutiveErrors = 0;
      fallback = false;
      reloadIssued = false;
      try {
        source = eventSourceFactory(
          options.url,
          options.withCredentials ?? false,
        );
        source.onopen = () => {
          if (startGeneration === generation) onOpen();
        };
        source.onerror = () => {
          if (startGeneration === generation) onError();
        };
        source.addEventListener('revision', (event) => {
          if (startGeneration === generation) onRevision(event);
        });
        source.addEventListener('heartbeat', (event) => {
          if (startGeneration === generation) onHeartbeat(event);
        });
      } catch {
        source?.close();
        source = null;
        fallback = true;
        scheduleRead('unconditional');
        schedulePoll();
      }
      scheduleSilenceCheck();
    },
    stop: () => {
      if (!running) return;
      running = false;
      generation += 1;
      clearSilentTimer();
      clearPollTimer();
      clearRepairTimer();
      queuedMode = null;
      reading = false;
      const current = source;
      source = null;
      current?.close();
    },
  };
}
