import type { Ref } from 'vue';
import { onScopeDispose, ref, watch } from 'vue';

export type StampState = 'idle' | 'landing' | 'lifting';

const LANDING_MS = 180;
const LIFTING_MS = 120;
const REDUCED_MOTION_QUERY = '(prefers-reduced-motion: reduce)';

export function useStamp(publicLink: Ref<string | null>): {
  stampState: Ref<StampState>;
  displayLink: Ref<string | null>;
} {
  const stampState = ref<StampState>('idle');
  const displayLink = ref<string | null>(publicLink.value);
  let timer: ReturnType<typeof setTimeout> | undefined;

  function clearTimer(): void {
    if (timer === undefined) return;
    clearTimeout(timer);
    timer = undefined;
  }

  watch(publicLink, (next, previous) => {
    if (next === previous) return;
    clearTimer();

    if (prefersReducedMotion()) {
      displayLink.value = next;
      stampState.value = 'idle';
      return;
    }

    if (next !== null) {
      displayLink.value = next;
      stampState.value = 'landing';
      timer = setTimeout(() => {
        stampState.value = 'idle';
        timer = undefined;
      }, LANDING_MS);
      return;
    }

    if (displayLink.value === null) {
      stampState.value = 'idle';
      return;
    }
    stampState.value = 'lifting';
    timer = setTimeout(() => {
      displayLink.value = null;
      stampState.value = 'idle';
      timer = undefined;
    }, LIFTING_MS);
  });

  onScopeDispose(clearTimer, true);
  return { stampState, displayLink };
}

function prefersReducedMotion(): boolean {
  return typeof globalThis.matchMedia === 'function'
    && globalThis.matchMedia(REDUCED_MOTION_QUERY).matches;
}
