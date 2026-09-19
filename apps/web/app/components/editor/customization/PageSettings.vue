<script setup lang="ts">
/**
 * `PageSettings` — the page size and margins used for the PDF and for
 * printing. Margins are presets over the stored millimetre pair; Custom
 * reveals the two axes. Letter pages show inches and store millimetres.
 */
import type { Customization } from '@aboutme/schema';
import { computed, ref } from 'vue';

import type { CustomizationDelta } from '../../../editor/commands';
import FormField from '../../app/FormField.vue';
import SelectField from '../../app/SelectField.vue';
import { Input } from '../../ui/input';
import {
  axisDeltas,
  effectiveMargin,
  formatLength,
  fromDisplay,
  MARGIN_PRESETS,
  type MarginAxis,
  type MarginChoice,
  marginChoice,
  MAX_MARGIN_MM,
  PAGE_SIZE_LABELS,
  type PageFormat,
  presetDeltas,
  presetLabel,
  PRINTABLE_EDGE_MM,
  toDisplay,
  unitFor,
} from './pageSettings';

const props = defineProps<{
  readonly customization: Customization;
}>();

const emit = defineEmits<{
  commit: [deltas: readonly CustomizationDelta[]];
}>();

const customRequested = ref(false);
const axisErrors = ref<Partial<Record<MarginAxis, string>>>({});

const format = computed(() => props.customization.pageFormat);
const stored = computed(() => props.customization.spacing.pageMargin);
const margin = computed(() => effectiveMargin(stored.value));
const choice = computed<MarginChoice>(() =>
  customRequested.value ? 'custom' : marginChoice(stored.value));
const unit = computed(() => unitFor(format.value));

const sizeOptions = (['a4', 'letter'] as const).map((value) => ({
  value,
  label: PAGE_SIZE_LABELS[value],
}));
const marginOptions = computed(() => [
  ...MARGIN_PRESETS.map(({ choice: value }) => ({
    value,
    label: presetLabel(value, format.value),
  })),
  { value: 'custom', label: 'Custom' },
]);
const axes = computed(() => [
  { axis: 'x' as const, label: `Left and right (${unit.value})` },
  { axis: 'y' as const, label: `Top and bottom (${unit.value})` },
]);
const edgeHint = computed(() =>
  margin.value.x < PRINTABLE_EDGE_MM || margin.value.y < PRINTABLE_EDGE_MM
    ? 'Most printers cannot print within '
    + `${formatLength(PRINTABLE_EDGE_MM, format.value)} of the paper edge.`
    : undefined);

function changeFormat(value: string): void {
  if (value !== 'a4' && value !== 'letter') return;
  if (value === format.value) return;
  axisErrors.value = {};
  emit('commit', [{ op: 'set', path: 'pageFormat', value }]);
}

function changeChoice(value: string): void {
  const next = value as MarginChoice;
  axisErrors.value = {};
  customRequested.value = next === 'custom';
  const deltas = presetDeltas(next, stored.value);
  if (deltas.length > 0) emit('commit', deltas);
}

function changeAxis(axis: MarginAxis, event: Event): void {
  const text = (event.target as HTMLInputElement).value.trim();
  const mm = text === ''
    ? undefined
    : fromDisplay(Number(text), format.value);
  if (mm === undefined) {
    axisErrors.value = {
      ...axisErrors.value,
      [axis]: `Enter a value from 0 to ${maxLabel(format.value)}.`,
    };
    return;
  }
  axisErrors.value = { ...axisErrors.value, [axis]: undefined };
  const deltas = axisDeltas(axis, mm, stored.value);
  if (deltas.length > 0) emit('commit', deltas);
}

function maxLabel(value: PageFormat): string {
  return `${toDisplay(MAX_MARGIN_MM, value)} ${unitFor(value)}`;
}
</script>

<template>
  <div class="grid gap-4">
    <p class="text-sm text-muted-foreground">
      Used for the PDF and printing. The web page adapts to the screen.
    </p>
    <SelectField
      id="customization-pageFormat"
      label="Page size"
      :model-value="format"
      name="pageFormat"
      :options="sizeOptions"
      @update:model-value="changeFormat"
    />
    <SelectField
      id="customization-spacing-pageMargin"
      :hint="choice === 'custom' ? undefined : edgeHint"
      label="Margins"
      :model-value="choice"
      name="spacing.pageMargin"
      :options="marginOptions"
      @update:model-value="changeChoice"
    />
    <div
      v-if="choice === 'custom'"
      class="grid gap-4"
      data-page-margin-custom
    >
      <FormField
        v-for="item in axes"
        :id="`customization-spacing-pageMargin-${item.axis}`"
        :key="item.axis"
        v-slot="{ id, describedBy, invalid }"
        :error="axisErrors[item.axis]"
        :label="item.label"
        :name="`spacing.pageMargin.${item.axis}`"
      >
        <Input
          :id="id"
          :aria-describedby="describedBy"
          :aria-invalid="invalid"
          inputmode="decimal"
          :max="toDisplay(MAX_MARGIN_MM, format)"
          min="0"
          :model-value="toDisplay(margin[item.axis], format)"
          step="any"
          type="number"
          @change="changeAxis(item.axis, $event)"
        />
      </FormField>
      <p
        v-if="edgeHint !== undefined"
        class="text-sm text-muted-foreground"
        data-page-margin-edge
      >
        {{ edgeHint }}
      </p>
    </div>
  </div>
</template>
