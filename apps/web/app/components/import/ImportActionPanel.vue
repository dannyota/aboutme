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
import { MAX_REQUEST_KB, requestKb } from '@/import/linkedin/pageState';

const props = defineProps<{
  readonly copy: ImportCopy;
  readonly createCopy: ResumeCreateCopy;
  readonly n: number;
  readonly s: number;
  readonly bytes: number;
  readonly sizeOver: boolean;
  readonly schemaBlocked: boolean;
  readonly creating: boolean;
  /** A resolved create-error message (cap, createFailed, retryLater, or
   * sessionLost), or null. `uncertain` is separate: it keeps its own
   * `returnToResumes` link (docs/design/linkedin-import-ui.md, "After the
   * request"). */
  readonly createErrorMessage: string | null;
  readonly uncertain: boolean;
}>();

const kb = computed(() => requestKb(props.bytes));
const meterFillClass = computed(() =>
  props.sizeOver ? 'bg-destructive' : 'bg-primary');
const blockReasonId = 'import-panel-block-reason';
const blocked = computed(() => props.sizeOver || props.schemaBlocked);

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
  <div class="grid gap-3 rounded-lg border bg-card p-6 shadow-product">
    <h2 class="hidden text-base font-semibold min-[900px]:block">
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
      class="text-sm text-destructive"
    >
      {{ sizeOver ? copy.sizeOver(MAX_REQUEST_KB) : copy.invalid }}
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
        :aria-describedby="blocked ? blockReasonId : undefined"
        :aria-disabled="blocked || undefined"
        class="w-full max-[899px]:order-2 max-[899px]:flex-1"
        data-action="import-create"
        :disabled="creating"
        type="submit"
      >
        {{ creating ? createCopy.creating : createCopy.createAndOpen }}
      </Button>
      <NuxtLink
        :aria-disabled="creating || undefined"
        :class="[
          buttonVariants({ variant: 'outline' }), 'w-full',
          'max-[899px]:order-1',
          creating && 'pointer-events-none opacity-50',
        ]"
        to="/app/resumes"
      >
        {{ createCopy.cancel }}
      </NuxtLink>
    </div>
  </div>
</template>
