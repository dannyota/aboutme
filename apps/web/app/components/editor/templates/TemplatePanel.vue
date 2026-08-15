<script setup lang="ts">
import { TEMPLATES, type TemplatePreset } from '@aboutme/schema/templates';
import { computed, ref } from 'vue';

import type { ResumeEditorActions } from '../../../composables/useResumeEditor';
import type {
  TemplateGroupCommand,
  TemplateGroupState,
} from '../../../editor/templateGroup';
import { templateUndoAvailable } from '../../../editor/templateGroup';
import type { ResumeRecord } from '../../../stores/resumes';
import TemplatePartialDialog from './TemplatePartialDialog.vue';

const props = defineProps<{
  readonly actions: ResumeEditorActions;
  readonly group?: TemplateGroupCommand;
  readonly record?: ResumeRecord;
  readonly state?: TemplateGroupState;
}>();

const notice = ref('');
const record = computed(() => props.record ?? props.actions.record.value);
const group = computed(() => props.group ?? groupFrom(record.value));
const state = computed(() => props.state ?? record.value?.templateState);
const preview = computed(() => group.value?.intendedFinal);
const canUndo = computed(() => {
  const undo = state.value?.kind === 'complete' ? state.value.undo : undefined;
  const current = record.value?.current;
  return undo !== undefined
    && current !== undefined
    && templateUndoAvailable(undo, current);
});

function apply(preset: Readonly<TemplatePreset>): void {
  const result = props.actions.applyTemplate(preset);
  if (result.kind === 'no-change') notice.value = 'No changes';
  if (result.kind === 'enqueued') notice.value = '';
}

function status(): string {
  if (state.value === undefined || state.value === null) return notice.value;
  switch (state.value.kind) {
    case 'queued':
    case 'running':
      return 'Saving template';
    case 'complete':
      return 'Template saved';
    case 'partial':
      return 'Template needs attention';
    default:
      return assertNever(state.value);
  }
}

function hasFormatWarning(preset: Readonly<TemplatePreset>): boolean {
  const customization = record.value?.current.document.customization;
  return (
    customization !== undefined
    && (preset.customization.pageFormat !== customization.pageFormat
      || preset.customization.dateFormat !== customization.dateFormat)
  );
}

function hasBaseSizeWarning(preset: Readonly<TemplatePreset>): boolean {
  return preset.customization.font.baseSizePx === 10;
}

function hasMarginWarning(preset: Readonly<TemplatePreset>): boolean {
  const margin = preset.customization.spacing.pageMargin;
  return margin !== undefined && (margin.x < 5 || margin.y < 5);
}

function groupFrom(
  record: ResumeRecord | undefined,
): TemplateGroupCommand | undefined {
  const candidates = [record?.attempt?.queueItem, ...(record?.pending ?? [])];
  return candidates.find(
    (candidate): candidate is TemplateGroupCommand =>
      candidate?.kind === 'templateGroup',
  );
}

function assertNever(value: never): never {
  throw new Error(`Unexpected template state: ${String(value)}`);
}
</script>

<template>
  <section aria-labelledby="template-title">
    <h2 id="template-title">
      Templates
    </h2>
    <p
      v-if="status() !== ''"
      role="status"
    >
      {{ status() }}
    </p>
    <ul aria-label="Template presets">
      <li
        v-for="preset in TEMPLATES"
        :key="preset.id"
        :data-template="preset.id"
        @click.self="apply(preset)"
      >
        <h3>{{ preset.name }}</h3>
        <p>{{ preset.description }}</p>
        <ul aria-label="Template warnings">
          <li v-if="hasFormatWarning(preset)">
            Page or date format will change.
          </li>
          <li v-if="hasBaseSizeWarning(preset)">
            This template uses a 10 pt base size.
          </li>
          <li v-if="hasMarginWarning(preset)">
            This template sets margins below 5 mm.
          </li>
        </ul>
        <button
          type="button"
          @click="apply(preset)"
        >
          Apply
        </button>
      </li>
    </ul>
    <p
      v-if="preview !== undefined && state?.kind !== 'partial'"
      data-template-preview
    >
      Template changes are ready to save.
    </p>
    <button
      v-if="canUndo && state?.kind === 'complete'"
      type="button"
      data-action="undo-template"
      @click="actions.undoTemplate()"
    >
      Undo template changes
    </button>
    <TemplatePartialDialog
      v-if="group !== undefined && state?.kind === 'partial'"
      :actions="actions"
      :group="group"
      :state="state"
    />
  </section>
</template>
