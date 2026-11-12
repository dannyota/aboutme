import { ref, type Ref } from 'vue';

import { retryDelay } from '../editor/coordinatorGuards';
import type { AuthState } from './useAuth';

/**
 * The editor's own session check after `/api/v1/me` failed with something
 * other than a 401. While it fails the editor stays open with its work; the
 * check runs again on the retry schedule in docs/design/budgets.md, and
 * `retry` runs it at once.
 */
export interface SessionCheck {
  /** The last check failed and another is due. */
  readonly failing: Readonly<Ref<boolean>>;
  /** Starts checking again after a failed check. */
  start(): void;
  /** Runs the check now. */
  retry(): Promise<void>;
  /** Ends checking: the session recovered, was lost, or the editor closed. */
  stop(): void;
}

export function createSessionCheck(deps: {
  readonly check: () => Promise<void>;
  readonly authState: Readonly<Ref<AuthState>>;
}): SessionCheck {
  const failing = ref(false);
  let failures = 0;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let checking = false;

  const clear = (): void => {
    if (timer !== undefined) clearTimeout(timer);
    timer = undefined;
  };
  const arm = (): void => {
    clear();
    timer = setTimeout(() => {
      timer = undefined;
      void run();
    }, retryDelay(failures));
    failures += 1;
  };
  async function run(): Promise<void> {
    if (!failing.value || checking) return;
    clear();
    checking = true;
    try {
      await deps.check();
    } catch {
      // A check that throws leaves the session state as it was.
    } finally {
      checking = false;
    }
    if (failing.value && deps.authState.value === 'error') arm();
  }

  return {
    failing,
    start: () => {
      if (failing.value) return;
      failing.value = true;
      failures = 0;
      arm();
    },
    retry: run,
    stop: () => {
      failing.value = false;
      failures = 0;
      clear();
    },
  };
}
