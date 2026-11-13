import { computed, ref, type Ref } from 'vue';

import { RETRY_FIRST_MS, retryDelay } from '../editor/coordinatorGuards';
import type { AuthState } from './useAuth';

/**
 * The editor's own session check after `/api/v1/me` failed with something
 * other than a 401. While it fails the editor stays open with its work; the
 * check runs again on the retry schedule in docs/design/budgets.md, and
 * `retry` runs it at once. Automatic and manual checks share one deadline:
 * none runs sooner than a second after a failed one, or before the server's
 * `Retry-After`, and a manual check moves the schedule on rather than
 * starting it over.
 */
export interface SessionCheck {
  /** The last check failed and another is due. */
  readonly failing: Readonly<Ref<boolean>>;
  /** A check is on its way; Try again waits for it. */
  readonly busy: Readonly<Ref<boolean>>;
  /** Starts checking again after a failed check. */
  start(): void;
  /** Runs the check now, or as soon as the server allows. */
  retry(): Promise<void>;
  /** Ends checking: the session's answer came, or the editor closed. */
  stop(): void;
}

export function createSessionCheck(deps: {
  readonly check: () => Promise<void>;
  readonly authState: Readonly<Ref<AuthState>>;
  /** The wait the last failed check asked for, if any. */
  readonly retryAfterMs?: () => number | null;
}): SessionCheck {
  const failing = ref(false);
  const checking = ref(false);
  let failures = 0;
  /** Epoch ms before which no check, automatic or manual, may run. */
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
