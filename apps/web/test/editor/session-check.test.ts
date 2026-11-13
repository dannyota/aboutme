import { ref } from 'vue';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { createSessionCheck } from '../../app/composables/sessionCheck';

type AuthState = 'authenticated' | 'anonymous' | 'error' | 'loading';

function failingCheck() {
  const authState = ref<AuthState>('error');
  const checkedAt: number[] = [];
  const check = vi.fn(async () => {
    checkedAt.push(Date.now());
  });
  return { authState, checkedAt, check };
}

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(0);
});

afterEach(() => {
  vi.useRealTimers();
});

describe('the session check', () => {
  it('runs again at 1 s, doubling to 30 s, while it keeps failing',
    async () => {
      const { authState, checkedAt, check } = failingCheck();
      const sessionCheck = createSessionCheck({ check, authState });

      sessionCheck.start();
      await vi.advanceTimersByTimeAsync(121_000);

      expect(checkedAt).toEqual([
        1_000, 3_000, 7_000, 15_000, 31_000, 61_000, 91_000, 121_000,
      ]);
      expect(sessionCheck.failing.value).toBe(true);
    });

  it('runs at once on Try again a second after a failed check, and keeps '
    + 'the schedule after it', async () => {
    const { authState, checkedAt, check } = failingCheck();
    const sessionCheck = createSessionCheck({ check, authState });
    sessionCheck.start();
    await vi.advanceTimersByTimeAsync(2_500);

    await sessionCheck.retry();
    await vi.advanceTimersByTimeAsync(4_500);

    expect(checkedAt).toEqual([1_000, 2_500, 6_500]);
  });

  it('sends no check sooner than a second after a failed one, however often '
    + 'Try again is clicked', async () => {
    const { authState, checkedAt, check } = failingCheck();
    const sessionCheck = createSessionCheck({ check, authState });
    sessionCheck.start();
    await vi.advanceTimersByTimeAsync(1_000);

    for (const at of [1_100, 1_200, 1_300, 1_400]) {
      vi.setSystemTime(at);
      await sessionCheck.retry();
    }
    await vi.advanceTimersByTimeAsync(1_000);

    expect(checkedAt).toEqual([1_000, 2_000]);
    expect(sessionCheck.busy.value).toBe(false);
  });

  it('waits for the server\'s Retry-After, Try again included', async () => {
    const { authState, checkedAt, check } = failingCheck();
    const sessionCheck = createSessionCheck({
      check,
      authState,
      retryAfterMs: () => 5_000,
    });
    sessionCheck.start();
    await vi.advanceTimersByTimeAsync(500);

    await sessionCheck.retry();
    await vi.advanceTimersByTimeAsync(5_500);
    await sessionCheck.retry();
    await vi.advanceTimersByTimeAsync(4_000);

    expect(checkedAt).toEqual([5_000, 10_000]);
  });

  it('runs no more once the check succeeds or is stopped', async () => {
    const { authState, checkedAt, check } = failingCheck();
    check.mockImplementationOnce(async () => {
      checkedAt.push(Date.now());
      authState.value = 'authenticated';
    });
    const sessionCheck = createSessionCheck({ check, authState });
    sessionCheck.start();

    await vi.advanceTimersByTimeAsync(60_000);
    expect(checkedAt).toEqual([1_000]);

    sessionCheck.stop();
    authState.value = 'error';
    await vi.advanceTimersByTimeAsync(60_000);
    expect(checkedAt).toEqual([1_000]);
    expect(sessionCheck.failing.value).toBe(false);
  });
});
