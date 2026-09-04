import { effectScope, nextTick, ref } from 'vue';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { useStamp } from '../../app/composables/useStamp';

beforeEach(() => {
  vi.useFakeTimers();
  vi.stubGlobal('matchMedia', vi.fn(() => ({ matches: false })));
});

afterEach(() => {
  vi.runOnlyPendingTimers();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe('useStamp', () => {
  it('lands a new canonical link for 180 ms', async () => {
    const link = ref<string | null>(null);
    const scope = effectScope();
    const stamp = scope.run(() => useStamp(link))!;

    link.value = '/ada-lovelace';
    await nextTick();

    expect(stamp.displayLink.value).toBe('/ada-lovelace');
    expect(stamp.stampState.value).toBe('landing');

    vi.advanceTimersByTime(179);
    expect(stamp.stampState.value).toBe('landing');
    vi.advanceTimersByTime(1);
    expect(stamp.stampState.value).toBe('idle');
    scope.stop();
  });

  it('retains a lifted link for 120 ms before clearing it', async () => {
    const link = ref<string | null>('/ada-lovelace');
    const scope = effectScope();
    const stamp = scope.run(() => useStamp(link))!;

    link.value = null;
    await nextTick();

    expect(stamp.displayLink.value).toBe('/ada-lovelace');
    expect(stamp.stampState.value).toBe('lifting');
    vi.advanceTimersByTime(120);
    expect(stamp.displayLink.value).toBeNull();
    expect(stamp.stampState.value).toBe('idle');
    scope.stop();
  });

  it('lands again when one canonical link replaces another', async () => {
    const link = ref<string | null>('/old-slug');
    const scope = effectScope();
    const stamp = scope.run(() => useStamp(link))!;

    link.value = '/new-slug';
    await nextTick();

    expect(stamp.displayLink.value).toBe('/new-slug');
    expect(stamp.stampState.value).toBe('landing');
    scope.stop();
  });

  it('changes immediately when reduced motion is preferred', async () => {
    vi.stubGlobal('matchMedia', vi.fn(() => ({ matches: true })));
    const link = ref<string | null>(null);
    const scope = effectScope();
    const stamp = scope.run(() => useStamp(link))!;

    link.value = '/ada-lovelace';
    await nextTick();
    expect(stamp.displayLink.value).toBe('/ada-lovelace');
    expect(stamp.stampState.value).toBe('idle');

    link.value = null;
    await nextTick();
    expect(stamp.displayLink.value).toBeNull();
    expect(stamp.stampState.value).toBe('idle');
    scope.stop();
  });
});
