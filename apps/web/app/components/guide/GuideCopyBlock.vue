<script setup lang="ts">
/**
 * One URL or command block (docs/design/mcp-guide.md, "Layout"): the
 * verify page's command block look, `--muted` fill, border, 10 px radius,
 * 13 px monospace, sideways scroll, and a 32 px copy button at the top
 * right. The URL block carries no `$` prompt; a shell command does
 * (docs/design/mcp-guide.md says the URL never gets one, and Claude Code
 * steps show real shell commands).
 */
import { Check, Copy } from '@lucide/vue';
import { useClipboardCopy } from '@/components/verify/useClipboardCopy';
import { shellPrompt } from '@/i18n/guide';

const props = withDefaults(defineProps<{
  readonly text: string;
  readonly copyLabel: string;
  readonly scrollLabel: string;
  readonly copiedText: string;
  readonly copyFailedText: string;
  readonly testid: string;
  readonly prompt?: boolean;
}>(), {
  prompt: false,
});

const { state, copy: copyToClipboard } = useClipboardCopy();

function onCopy(): void {
  void copyToClipboard(props.text, props.copiedText, props.copyFailedText);
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
    ><code
      translate="no"
    ><span
      v-if="prompt"
      class="select-none text-muted-foreground"
    >{{ shellPrompt }}</span>{{ text }}</code></pre>
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
