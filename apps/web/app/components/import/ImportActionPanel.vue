<script setup lang="ts">
/**
 * The review's action panel: the size meter, the block reason, create
 * errors, and the Create and Cancel actions (docs/design/
 * linkedin-import-ui.md, "Action panel").
 */
import { computed, ref, watch } from 'vue';
import StatusBanner from '@/components/app/StatusBanner.vue';
import { Button, buttonVariants } from '@/components/ui/button';
import type { ImportCopy } from '@/i18n/import';
import type { ResumeCreateCopy } from '@/i18n/resume-create';
import type { SchemaCheck } from '@/import/linkedin/build';
import { MAX_REQUEST_KB, requestKb } from '@/import/linkedin/pageState';

const props = defineProps<{
  readonly copy: ImportCopy;
  readonly createCopy: ResumeCreateCopy;
  readonly n: number;
  readonly s: number;
  readonly bytes: number;
  readonly sizeOver: boolean;
  readonly schemaCheck: SchemaCheck;
  readonly creating: boolean;
  /** A resolved create-error message (cap, createFailed, retryLater, or
   * sessionLost), or null. `uncertain` is separate: it keeps its own
   * `returnToResumes` link (docs/design/linkedin-import-ui.md, "After the
   * request"). */
  readonly createErrorMessage: string | null;
  readonly uncertain: boolean;
}>();

// Both Cancel variants (fix 6: a disabled button while creating, not a
// styled link) share the same layout and DOM-order classes.
const CANCEL_CLASS = 'w-full min-[900px]:order-2';

const kb = computed(() => requestKb(props.bytes));
const meterFillClass = computed(() =>
  props.sizeOver ? 'bg-destructive' : 'bg-primary');
const blockReasonId = 'import-panel-block-reason';
const schemaBlocked = computed(() => !props.schemaCheck.ok);
// A schema failure outside any entry or detail has no row to point to, so
// the reason names the whole document instead of asking to deselect
// entries (docs/design/linkedin-import-ui.md, "Action panel").
const schemaGeneralOnly = computed(() =>
  !props.schemaCheck.ok && props.schemaCheck.entryIds.length === 0);
const blocked = computed(() => props.sizeOver || schemaBlocked.value);

const reasonRef = ref<HTMLParagraphElement>();
defineExpose({ focusReason: (): void => reasonRef.value?.focus() });

// Announces once per crossing, per "Accessibility": sizeOver when the size
// passes the limit, sizeOk when it drops back below it.
const announcement = ref('');
watch(() => props.sizeOver, (over, wasOver) => {
  if (wasOver === undefined) return;
  announcement.value = over
    ? props.copy.sizeOver(MAX_REQUEST_KB)
    : props.copy.sizeOk;
});
</script>

<template>
  <aside
    aria-labelledby="import-panel-heading"
    class="sticky bottom-0 -mx-4 grid gap-3 border-t bg-card px-4 pt-3
      pb-[max(12px,env(safe-area-inset-bottom))] shadow-product
      sm:-mx-6
      min-[900px]:bottom-auto min-[900px]:top-6 min-[900px]:mx-0
      min-[900px]:self-start min-[900px]:rounded-lg min-[900px]:border
      min-[900px]:p-6"
  >
    <h2
      id="import-panel-heading"
      class="sr-only text-base font-semibold min-[900px]:not-sr-only"
    >
      {{ copy.panelHeading }}
    </h2>
    <p class="text-sm max-[899px]:text-xs">
      {{ copy.selectedCount(n, s) }}
    </p>

    <div class="grid gap-1">
      <div class="flex justify-between text-sm max-[899px]:text-xs">
        <span>{{ copy.sizeLabel }}</span>
        <span>{{ copy.sizeValue(kb, MAX_REQUEST_KB) }}</span>
      </div>
      <div
        :aria-label="copy.sizeLabel"
        aria-valuemax="262144"
        aria-valuemin="0"
        :aria-valuenow="bytes"
        :aria-valuetext="copy.sizeValue(kb, MAX_REQUEST_KB)"
        class="h-1.5 overflow-hidden rounded-full bg-muted"
        role="meter"
      >
        <div
          class="h-full rounded-full"
          :class="meterFillClass"
          :style="{ width: `${Math.min(100, (bytes / 262144) * 100)}%` }"
        />
      </div>
    </div>
    <span
      aria-live="polite"
      class="sr-only"
    >{{ announcement }}</span>

    <p
      v-if="blocked"
      :id="blockReasonId"
      ref="reasonRef"
      class="text-sm text-destructive"
      tabindex="-1"
    >
      {{ sizeOver
        ? copy.sizeOver(MAX_REQUEST_KB)
        : (schemaGeneralOnly ? copy.invalidGeneral : copy.invalid) }}
    </p>

    <StatusBanner
      v-if="createErrorMessage !== null"
      data-import-create-error
      kind="error"
    >
      {{ createErrorMessage }}
    </StatusBanner>
    <template v-if="uncertain">
      <StatusBanner
        data-import-create-error
        kind="error"
      >
        {{ createCopy.uncertain }}
      </StatusBanner>
      <NuxtLink
        :class="buttonVariants({ variant: 'outline' })"
        to="/app/resumes"
      >
        {{ createCopy.returnToResumes }}
      </NuxtLink>
    </template>

    <div class="grid gap-2 min-[900px]:grid-cols-1 max-[899px]:grid-flow-col">
      <Button
        v-if="creating"
        :class="CANCEL_CLASS"
        data-action="import-cancel"
        disabled
        type="button"
        variant="outline"
      >
        {{ createCopy.cancel }}
      </Button>
      <NuxtLink
        v-else
        :class="[buttonVariants({ variant: 'outline' }), CANCEL_CLASS]"
        data-action="import-cancel"
        to="/app/resumes"
      >
        {{ createCopy.cancel }}
      </NuxtLink>
      <Button
        :aria-describedby="blocked ? blockReasonId : undefined"
        :aria-disabled="blocked || undefined"
        class="w-full max-[899px]:flex-1"
        data-action="import-create"
        :disabled="creating"
        type="submit"
      >
        {{ creating ? createCopy.creating : createCopy.createAndOpen }}
      </Button>
    </div>
  </aside>
</template>
