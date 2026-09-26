<script setup lang="ts">
/**
 * The digest chip and its copy button (docs/design/deployment-transparency/
 * visual.md, "Digest chip and copy"): a short, never-wrapping monospace
 * digest with the full value in an accessible name, a tooltip, and a real
 * copy button that confirms through the page's shared announcer.
 */
import { Check, Copy } from '@lucide/vue';
import { computed } from 'vue';
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip';
import type { ComponentName } from '@/utils/deploymentDocument';
import { shortDigest } from '@/utils/verifyView';
import type { Locale } from '@/i18n/locale';
import { verifyCopy } from '@/i18n/verify';
import { cn } from '@/lib/utils';
import { useClipboardCopy } from './useClipboardCopy';

const props = defineProps<{
  readonly digest: string;
  readonly component: ComponentName;
  readonly version: string | null;
  readonly locale: Locale;
  readonly failing?: boolean;
}>();

const copy = computed(() => verifyCopy[props.locale]);
const short = computed(() => shortDigest(props.digest));
const label = computed(() =>
  copy.value.components.copyDigest(props.component, props.version));
const { state, copy: copyToClipboard } = useClipboardCopy();

function onCopy(): void {
  void copyToClipboard(props.digest, copy.value.copied, copy.value.copyFailed);
}
</script>

<template>
  <span class="inline-flex flex-none items-center whitespace-nowrap">
    <TooltipProvider>
      <Tooltip :open="state === 'failed' ? true : undefined">
        <TooltipTrigger as-child>
          <span
            :class="cn(
              'inline-flex h-8 items-center rounded-l-lg border px-2',
              'bg-muted font-mono text-[13px]',
              failing && 'border-destructive',
            )"
            :title="digest"
          >
            <span aria-hidden="true">{{ short }}</span>
            <span class="sr-only">{{ digest }}</span>
          </span>
        </TooltipTrigger>
        <TooltipContent>{{ digest }}</TooltipContent>
      </Tooltip>
    </TooltipProvider>
    <button
      :aria-label="label"
      :class="cn(
        'inline-flex h-8 w-8 flex-none items-center justify-center',
        'rounded-r-lg border border-l-0 bg-muted',
        failing && 'border-destructive',
      )"
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
    <span
      v-if="state === 'copied'"
      class="ml-2 text-xs font-semibold text-link"
    >{{ copy.copied }}</span>
  </span>
</template>
