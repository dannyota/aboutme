<script setup lang="ts">
/**
 * `PageSettings` — the page size and margins used for the PDF and for
 * printing. Margins are presets over the stored millimetre pair; Custom
 * reveals the two axes. Letter pages show inches and store millimetres.
 */
import type { Customization } from '@aboutme/schema';
import { computed, ref, watch } from 'vue';

import type { CustomizationDelta } from '../../../editor/commands';
import {
  editorControlsCopy,
  pageSizeLabel,
} from '../../../i18n/editor-controls';
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
  marginLabel,
  MAX_MARGIN_MM,
  type PageFormat,
  presetDeltas,
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
const { locale } = useLocale();
const copy = computed(() => editorControlsCopy[locale.value]);

const customRequested = ref(false);
const invalidAxes = ref<Partial<Record<MarginAxis, boolean>>>({});
const axisErrors = computed<Partial<Record<MarginAxis, string>>>(() => {
  const maximum = maxLabel(format.value);
  return Object.fromEntries(
    (['x', 'y'] as const).flatMap((axis) =>
      invalidAxes.value[axis] ? [[axis, copy.value.page.invalid(maximum)]] : [],
    ),
  );
});
const axisDrafts = ref<Record<MarginAxis, string>>({ x: '', y: '' });

const format = computed(() => props.customization.pageFormat);
const stored = computed(() => props.customization.spacing.pageMargin);
const margin = computed(() => effectiveMargin(stored.value));
const choice = computed<MarginChoice>(() =>
  customRequested.value ? 'custom' : marginChoice(stored.value),
);
const unit = computed(() => unitFor(format.value));

watch(
  [margin, format],
  () => {
    axisDrafts.value = {
      x: String(toDisplay(margin.value.x, format.value)),
      y: String(toDisplay(margin.value.y, format.value)),
    };
  },
  { immediate: true },
);

const sizeOptions = (['a4', 'letter'] as const).map((value) => ({
  value,
  label: pageSizeLabel(locale.value, value),
}));
const marginOptions = computed(() => [
  ...MARGIN_PRESETS.map(({ choice: value }) => ({
    value,
    label: copy.value.page.preset(
      locale.value === 'vi'
        ? marginLabel(value, 'vi')
        : marginLabel(value),
      formatLength(
        MARGIN_PRESETS.find((item) => item.choice === value)!.mm,
        format.value,
      ),
    ),
  })),
  { value: 'custom', label: copy.value.page.custom },
]);
const axes = computed(() => [
  { axis: 'x' as const, label: copy.value.page.horizontal(unit.value) },
  { axis: 'y' as const, label: copy.value.page.vertical(unit.value) },
]);
const edgeHint = computed(() =>
  margin.value.x < PRINTABLE_EDGE_MM || margin.value.y < PRINTABLE_EDGE_MM
    ? copy.value.page.edge(formatLength(PRINTABLE_EDGE_MM, format.value))
    : undefined,
);

function changeFormat(value: string): void {
  if (value !== 'a4' && value !== 'letter') return;
  if (value === format.value) return;
  invalidAxes.value = {};
  emit('commit', [{ op: 'set', path: 'pageFormat', value }]);
}

function changeChoice(value: string): void {
  const next = value as MarginChoice;
  invalidAxes.value = {};
  customRequested.value = next === 'custom';
  const deltas = presetDeltas(next, stored.value);
  if (deltas.length > 0) emit('commit', deltas);
}

function changeAxis(axis: MarginAxis, event: Event): void {
  const text = (event.target as HTMLInputElement).value.trim();
  axisDrafts.value = { ...axisDrafts.value, [axis]: text };
  const mm = text === '' ? undefined : fromDisplay(Number(text), format.value);
  if (mm === undefined) {
    invalidAxes.value = {
      ...invalidAxes.value,
      [axis]: true,
    };
    return;
  }
  invalidAxes.value = { ...invalidAxes.value, [axis]: false };
  const deltas = axisDeltas(axis, mm, stored.value);
  if (deltas.length > 0) emit('commit', deltas);
}

function changeAxisDraft(axis: MarginAxis, event: Event): void {
  axisDrafts.value = {
    ...axisDrafts.value,
    [axis]: (event.target as HTMLInputElement).value,
  };
}

function maxLabel(value: PageFormat): string {
  return `${toDisplay(MAX_MARGIN_MM, value)} ${unitFor(value)}`;
}
</script>

<template>
  <div class="grid gap-4">
    <p class="text-sm text-muted-foreground">
      {{ copy.page.description }}
    </p>
    <SelectField
      id="customization-pageFormat"
      :label="copy.page.pageSize"
      :model-value="format"
      name="pageFormat"
      :options="sizeOptions"
      @update:model-value="changeFormat"
    />
    <SelectField
      id="customization-spacing-pageMargin"
      :hint="choice === 'custom' ? undefined : edgeHint"
      :label="copy.page.margins"
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
          :model-value="axisDrafts[item.axis]"
          step="any"
          type="number"
          @input="changeAxisDraft(item.axis, $event)"
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
