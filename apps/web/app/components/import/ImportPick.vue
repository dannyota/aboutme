<script setup lang="ts">
/**
 * The Pick, Reading, and Failed states of /app/import/linkedin: the steps
 * card, the drop zone (which becomes the reading progress in place), and the
 * privacy note (docs/design/linkedin-import-ui.md, "Pick state" and
 * "Reading state").
 */
import { FileUp, Languages, LockKeyhole } from '@lucide/vue';
import { computed, ref, useTemplateRef } from 'vue';
import StatusBanner from '@/components/app/StatusBanner.vue';
import { Button } from '@/components/ui/button';
import type { ImportCopy } from '@/i18n/import';

const props = defineProps<{
  readonly copy: ImportCopy;
  /** The message key for the error banner above the drop zone, or null. */
  readonly error: string | null;
  /** Shows the `stopped` info banner in the same slot as `error`, since only
   * one banner shows at a time (docs/design/linkedin-import-ui.md,
   * "Messages"). */
  readonly stopped: boolean;
  readonly reading: boolean;
  readonly fileName: string | null;
  readonly pagesRead: number;
  readonly pageCount: number | null;
}>();
const emit = defineEmits<{
  pick: [file: File];
  stop: [];
  dropMultiple: [];
}>();

const input = useTemplateRef<HTMLInputElement>('input');
const chooseButton = useTemplateRef<{ $el?: HTMLElement }>('chooseButton');
const stopButton = useTemplateRef<{ $el?: HTMLElement }>('stopButton');
const dragOver = ref(false);

const errorText = computed(() => {
  const key = props.error;
  if (key === null) return null;
  return key in props.copy
    ? (props.copy as unknown as Record<string, string>)[key]
    : null;
});
const chooseLabel = computed(() =>
  props.error === null ? props.copy.choose : props.copy.chooseAnother);
const dropPrompt = computed(() =>
  dragOver.value ? props.copy.dropActive : props.copy.dropPrompt);
const progressMax = computed(() => props.pageCount ?? 0);

function openPicker(): void {
  input.value?.click();
}

function onInputChange(event: Event): void {
  const target = event.target as HTMLInputElement;
  const file = target.files?.[0];
  if (file !== undefined) emit('pick', file);
  // Cleared so the same file can be picked again (docs/design/
  // linkedin-import-ui.md, "Behavior").
  target.value = '';
}

/**
 * Dropping more than one file reads nothing and shows dropOne (docs/design/
 * linkedin-import-ui.md, "Pick state"); the caller passes that as `error`.
 */
function onDropFiles(event: DragEvent): void {
  dragOver.value = false;
  if (props.reading) return;
  const files = event.dataTransfer?.files;
  if (files === undefined || files.length === 0) return;
  if (files.length > 1) {
    emit('dropMultiple');
    return;
  }
  emit('pick', files[0]!);
}

defineExpose({
  focusChoose: (): void => chooseButton.value?.$el?.focus(),
  focusStop: (): void => stopButton.value?.$el?.focus(),
});
</script>

<template>
  <div class="grid gap-6">
    <section
      aria-labelledby="import-steps-heading"
      class="rounded-lg border bg-card p-4 shadow-product sm:p-6"
    >
      <h2
        id="import-steps-heading"
        class="text-base font-semibold"
      >
        {{ copy.stepsHeading }}
      </h2>
      <ol class="mt-3 grid gap-3">
        <li
          v-for="(step, index) in [
            copy.step1, copy.step2, copy.step3, copy.step4,
          ]"
          :key="index"
          class="flex items-start gap-3"
        >
          <span
            aria-hidden="true"
            class="flex size-6 shrink-0 items-center justify-center rounded-full
              bg-surface-blue text-sm font-semibold text-link"
          >{{ index + 1 }}</span>
          <span>{{ step }}</span>
        </li>
      </ol>
      <p class="mt-4 flex items-start gap-2 text-sm text-foreground">
        <Languages
          aria-hidden="true"
          class="mt-0.5 size-4 shrink-0 text-brand-indigo"
        />
        <span>{{ copy.englishOnly }}</span>
      </p>
    </section>

    <StatusBanner
      v-if="errorText !== null"
      class="-mb-2"
      data-import-error
      kind="error"
    >
      {{ errorText }}
    </StatusBanner>
    <StatusBanner
      v-else-if="stopped"
      class="-mb-2"
      data-import-stopped
      kind="info"
    >
      {{ copy.stopped }}
    </StatusBanner>

    <section
      aria-labelledby="import-pick-heading"
      class="rounded-lg border-2 border-dashed p-6 transition-colors
        duration-150 motion-reduce:transition-none sm:p-8"
      :class="dragOver
        ? 'border-primary bg-surface-indigo'
        : 'border-input bg-surface-blue'"
      @dragleave="dragOver = false"
      @dragover.prevent="dragOver = !reading"
      @drop.prevent="onDropFiles"
    >
      <h2
        id="import-pick-heading"
        class="sr-only"
      >
        {{ copy.pickHeading }}
      </h2>

      <!-- Both states stay mounted, stacked in the same grid cell, so the
        zone keeps the same height in Reading as in Pick (docs/design/
        linkedin-import-ui.md, "Reading state"). The inactive one is
        `invisible` (still sized) and `aria-hidden`, which also drops it from
        the tab order. -->
      <div class="grid">
        <div
          :aria-hidden="reading || undefined"
          class="col-start-1 row-start-1 grid justify-items-center gap-3
            text-center"
          :class="reading && 'invisible'"
        >
          <FileUp
            aria-hidden="true"
            class="size-8 text-brand-blue"
          />
          <p class="hidden font-medium sm:block">
            {{ dropPrompt }}
          </p>
          <p class="hidden text-muted-foreground sm:block">
            {{ copy.or }}
          </p>
          <Button
            ref="chooseButton"
            class="w-full sm:w-auto"
            data-action="import-choose"
            type="button"
            @click="openPicker"
          >
            {{ chooseLabel }}
          </Button>
          <p class="text-xs text-muted-foreground">
            {{ copy.limits }}
          </p>
        </div>

        <div
          :aria-hidden="!reading || undefined"
          class="col-start-1 row-start-1 grid gap-2"
          :class="!reading && 'invisible'"
        >
          <p
            class="truncate font-medium"
            :title="fileName ?? ''"
          >
            {{ fileName }}
          </p>
          <p
            role="status"
          >
            {{ copy.reading }}
          </p>
          <div
            :aria-label="copy.progressLabel"
            :aria-valuemax="progressMax"
            aria-valuemin="0"
            :aria-valuenow="pagesRead"
            class="h-1.5 overflow-hidden rounded-full bg-input"
            role="progressbar"
          >
            <div
              class="h-full rounded-full bg-primary transition-[width]
                duration-150 motion-reduce:transition-none"
              :style="{
                width: progressMax > 0
                  ? `${(pagesRead / progressMax) * 100}%`
                  : '0%',
              }"
            />
          </div>
          <p
            aria-hidden="true"
            class="text-sm text-muted-foreground"
          >
            {{
              pageCount === null ? '' : copy.readingPage(pagesRead, pageCount)
            }}
          </p>
          <p class="text-xs text-muted-foreground">
            {{ copy.readingNote }}
          </p>
          <Button
            ref="stopButton"
            :aria-label="copy.stopLabel"
            class="justify-self-start"
            data-action="import-stop"
            size="sm"
            type="button"
            variant="outline"
            @click="emit('stop')"
          >
            {{ copy.stop }}
          </Button>
        </div>
      </div>

      <input
        ref="input"
        accept="application/pdf,.pdf"
        aria-hidden="true"
        class="sr-only"
        data-testid="import-file-input"
        tabindex="-1"
        type="file"
        @change="onInputChange"
      >
    </section>

    <p class="flex items-start gap-2 text-sm text-muted-foreground">
      <LockKeyhole
        aria-hidden="true"
        class="mt-0.5 size-4 shrink-0 text-brand-blue"
      />
      <span>{{ copy.privacy }} {{ copy.ownProfile }}</span>
    </p>
  </div>
</template>
