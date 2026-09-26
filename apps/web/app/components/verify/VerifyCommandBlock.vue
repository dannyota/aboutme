<script setup lang="ts">
/**
 * One command block (docs/design/deployment-transparency/visual.md, "Verify
 * it yourself"): a horizontally scrolling `pre` with live values
 * highlighted, a `$` prompt that is never copied, and a copy button that
 * confirms through the page's shared announcer.
 */
import { Check, Copy } from '@lucide/vue';
import type { CommandPart } from '@/utils/verifyView';
import { useClipboardCopy } from './useClipboardCopy';

const props = defineProps<{
  readonly lines: readonly (readonly CommandPart[])[];
  readonly copyText: string;
  readonly copyLabel: string;
  readonly scrollLabel: string;
  readonly testid: string;
  readonly copiedText: string;
  readonly copyFailedText: string;
}>();

const { state, copy: copyToClipboard } = useClipboardCopy();

function onCopy(): void {
  void copyToClipboard(props.copyText, props.copiedText, props.copyFailedText);
}

function partClass(kind: CommandPart['kind']): string {
  if (kind === 'live') {
    return 'rounded-sm bg-surface-blue font-bold text-foreground';
  }
  if (kind === 'placeholder') return 'italic text-muted-foreground';
  return '';
}
</script>

<template>
  <div
    class="relative"
    :data-testid="testid"
  >
    <pre
      :aria-label="scrollLabel"
      class="overflow-x-auto rounded-[10px] border bg-muted p-3.5 pr-[52px]
        font-mono text-[13px] leading-[1.65]"
      tabindex="0"
    ><code><template
      v-for="(line, lineIndex) in lines"
      :key="lineIndex"
    ><span
      v-if="lineIndex === 0"
      class="select-none text-muted-foreground"
    >$ </span><span
      v-for="(part, partIndex) in line"
      :key="partIndex"
      :class="partClass(part.kind)"
    >{{ part.text }}</span>{{
      lineIndex < lines.length - 1 ? '\n' : ''
    }}</template></code></pre>
    <button
      :aria-label="copyLabel"
      class="absolute right-2 top-2 inline-flex h-8 w-8 items-center
        justify-center rounded-md border bg-card"
      type="button"
      @click="onCopy"
    >
      <Check
        v-if="state === 'copied'"
        aria-hidden="true"
        class="size-3.5 text-link"
      />
      <Copy
        v-else
        aria-hidden="true"
        class="size-3.5"
      />
    </button>
  </div>
</template>
