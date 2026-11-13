import { inject, onBeforeUnmount, ref } from 'vue';
import { verifyAnnounceKey } from './verifyAnnounce';

export type ClipboardState = 'idle' | 'copied' | 'failed';

const copiedDurationMs = 2_000;

export function useClipboardCopy() {
  const state = ref<ClipboardState>('idle');
  const announce = inject(verifyAnnounceKey, () => {});
  let resetTimer: ReturnType<typeof setTimeout> | undefined;

  function clearResetTimer(): void {
    if (resetTimer !== undefined) {
      clearTimeout(resetTimer);
      resetTimer = undefined;
    }
  }

  async function copy(
    text: string,
    copied: string,
    failed: string,
  ): Promise<void> {
    clearResetTimer();
    try {
      await navigator.clipboard.writeText(text);
      state.value = 'copied';
      announce(copied);
      resetTimer = setTimeout(() => {
        state.value = 'idle';
        resetTimer = undefined;
      }, copiedDurationMs);
    } catch {
      state.value = 'failed';
      announce(failed);
    }
  }

  onBeforeUnmount(clearResetTimer);

  return { state, copy };
}
