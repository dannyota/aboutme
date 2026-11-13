import { computed, ref, type Ref } from 'vue';

import { RETRY_FIRST_MS, retryDelay } from '../editor/coordinatorGuards';
import type { AuthState } from './useAuth';

export interface SessionCheck {
  readonly failing: Readonly<Ref<boolean>>;
  readonly busy: Readonly<Ref<boolean>>;
  start(): void;
  retry(): Promise<void>;
  stop(): void;
}

export function createSessionCheck(deps: {
  readonly check: () => Promise<void>;
  readonly authState: Readonly<Ref<AuthState>>;
  readonly retryAfterMs?: () => number | null;
}): SessionCheck {
  const failing = ref(false);
  const checking = ref(false);
  let failures = 0;
  let notBefore = 0;
  const deadline = (now: number): number =>
    now + Math.max(RETRY_FIRST_MS, deps.retryAfterMs?.() ?? 0);
  let timer: ReturnType<typeof setTimeout> | undefined;

  const clear = (): void => {
    if (timer !== undefined) clearTimeout(timer);
    timer = undefined;
  };
  const arm = (delayMs: number): void => {
    clear();
    timer = setTimeout(() => {
      timer = undefined;
      void run();
    }, delayMs);
  };
  async function run(): Promise<void> {
    if (!failing.value || checking.value) return;
    const wait = notBefore - Date.now();
    if (wait > 0) {
      arm(wait);
      return;
    }
    clear();
    checking.value = true;
    try {
      await deps.check();
    } catch {
      // A check that throws leaves the session state as it was.
    } finally {
      checking.value = false;
    }
    if (!failing.value || deps.authState.value !== 'error') return;
    const now = Date.now();
    notBefore = deadline(now);
    const backoff = retryDelay(failures);
    failures += 1;
    arm(Math.max(backoff, notBefore - now));
  }

  return {
    failing,
    busy: computed(() => checking.value),
    start: () => {
      if (failing.value) return;
      failing.value = true;
      failures = 0;
      const now = Date.now();
      notBefore = deadline(now);
      arm(Math.max(retryDelay(failures), notBefore - now));
      failures += 1;
    },
    retry: run,
    stop: () => {
      failing.value = false;
      failures = 0;
      notBefore = 0;
      clear();
    },
  };
}
