<script setup lang="ts">
import type { DateRange, YearMonth } from '@aboutme/schema';
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { editorFieldsCopy } from '@/i18n/editor-fields';
import { Button } from '@/components/ui/button';
import CheckboxField from '@/components/app/CheckboxField.vue';
import FormField from '@/components/app/FormField.vue';
import { Input } from '@/components/ui/input';

import { useFieldDrafts } from '../../../composables/useFieldDrafts';
import type { FieldIntent } from './fieldIntent';

const props = defineProps<{
  readonly fieldId: string;
  readonly modelValue?: DateRange;
}>();
const emit = defineEmits<{ intent: [intent: FieldIntent<DateRange>] }>();

interface DraftFields {
  readonly startYear: string;
  readonly startMonth: string;
  readonly endYear: string;
  readonly endMonth: string;
  readonly present: boolean;
}
type DateError = 'invalidStart' | 'missingEnd' | 'order';

// A range that cannot be saved yet (a start with no end) is held in the
// editor's draft registry, so a remount restores it instead of losing it.
const drafts = useFieldDrafts();
const draftKey = `dates:${props.fieldId}`;
const restored = drafts?.get(draftKey) as DraftFields | undefined;

const dirty = ref(restored !== undefined);
const endMonth = ref(restored?.endMonth ?? toText(props.modelValue?.end?.m));
const endYear = ref(restored?.endYear ?? toText(props.modelValue?.end?.y));
const error = ref<DateError>();
const { locale } = useLocale();
const copy = computed(() => editorFieldsCopy[locale.value].dates);
const errorCopy = computed(() =>
  error.value === undefined ? '' : copy.value[error.value],
);
const present = ref(restored?.present ?? props.modelValue?.present ?? false);
const startMonth = ref(
  restored?.startMonth ?? toText(props.modelValue?.start.m),
);
const startYear = ref(restored?.startYear ?? toText(props.modelValue?.start.y));

watch(
  [dirty, startYear, startMonth, endYear, endMonth, present],
  () => {
    if (drafts === undefined) return;
    if (!dirty.value) {
      drafts.clear(draftKey);
      return;
    }
    drafts.set(
      draftKey,
      {
        startYear: startYear.value,
        startMonth: startMonth.value,
        endYear: endYear.value,
        endMonth: endMonth.value,
        present: present.value,
      } satisfies DraftFields,
      commit,
    );
  },
  { immediate: true },
);

// A restored draft shows what it still needs; a valid one saves at once.
onMounted(() => {
  if (restored !== undefined) commit();
});
onBeforeUnmount(commit);

watch(
  () => props.modelValue,
  (next) => {
    if (dirty.value) return;
    startYear.value = toText(next?.start.y);
    startMonth.value = toText(next?.start.m);
    endYear.value = toText(next?.end?.y);
    endMonth.value = toText(next?.end?.m);
    present.value = next?.present ?? false;
  },
);

function capture(): void {
  dirty.value = true;
  error.value = undefined;
}

function changePresent(): void {
  capture();
  if (present.value) {
    endYear.value = '';
    endMonth.value = '';
  }
  commit();
}

function commit(): void {
  if (!dirty.value) return;
  if (allEmpty() && !present.value) {
    if (props.modelValue !== undefined) emit('intent', { kind: 'unset' });
    dirty.value = false;
    return;
  }
  const start = readYearMonth(startYear.value, startMonth.value);
  if (start === null) {
    if (allEmpty() && props.modelValue !== undefined && !present.value) {
      emit('intent', { kind: 'unset' });
      dirty.value = false;
      return;
    }
    error.value = 'invalidStart';
    return;
  }
  const end = present.value
    ? null
    : readYearMonth(endYear.value, endMonth.value);
  if (!present.value && end === null) {
    error.value = 'missingEnd';
    return;
  }
  if (end !== null && compareYearMonth(start, end) > 0) {
    error.value = 'order';
    return;
  }
  const candidate = { start, end, present: present.value };
  if (sameRange(candidate, props.modelValue)) {
    dirty.value = false;
    return;
  }
  emit('intent', {
    kind: 'set',
    value: candidate,
  });
  dirty.value = false;
}

function sameRange(left: DateRange, right: DateRange | undefined): boolean {
  return (
    right !== undefined
    && left.present === right.present
    && left.start.y === right.start.y
    && left.start.m === right.start.m
    && left.end?.y === right.end?.y
    && left.end?.m === right.end?.m
  );
}

function unset(): void {
  // Also discards an unfinished range that was never saved.
  dirty.value = false;
  error.value = undefined;
  startYear.value = '';
  startMonth.value = '';
  endYear.value = '';
  endMonth.value = '';
  present.value = false;
  if (props.modelValue !== undefined) emit('intent', { kind: 'unset' });
}

function allEmpty(): boolean {
  return (
    startYear.value === ''
    && startMonth.value === ''
    && endYear.value === ''
    && endMonth.value === ''
  );
}

function compareYearMonth(left: YearMonth, right: YearMonth): number {
  const leftMonth = left.m ?? 1;
  const rightMonth = right.m ?? 1;
  return left.y === right.y ? leftMonth - rightMonth : left.y - right.y;
}

function readYearMonth(yearText: string, monthText: string): YearMonth | null {
  const year = readInteger(yearText, 1900, 2100);
  if (year === null) return null;
  if (monthText === '') return { y: year };
  const month = readInteger(monthText, 1, 12);
  return month === null ? null : { y: year, m: month };
}

function readInteger(value: string, min: number, max: number): number | null {
  if (!/^-?\d+$/.test(value)) return null;
  const parsed = Number(value);
  return Number.isFinite(parsed)
    && Number.isInteger(parsed)
    && parsed >= min
    && parsed <= max
    ? parsed
    : null;
}

function toText(value: number | undefined): string {
  return value === undefined ? '' : String(value);
}
</script>

<template>
  <div
    role="group"
    :aria-labelledby="`${fieldId}-label`"
    :aria-describedby="error === undefined ? undefined : `${fieldId}-error`"
  >
    <div
      :id="`${fieldId}-label`"
      class="text-sm font-medium"
    >
      {{ copy.dateRange }}
    </div>
    <FormField :label="copy.startYear">
      <template #default="{ id, describedBy, invalid }">
        <Input
          :id="id"
          v-model="startYear"
          :aria-describedby="describedBy"
          :aria-invalid="invalid"
          data-part="start-year"
          inputmode="numeric"
          @input="capture"
          @blur="commit"
        />
      </template>
    </FormField>
    <FormField :label="copy.startMonth">
      <template #default="{ id, describedBy, invalid }">
        <Input
          :id="id"
          v-model="startMonth"
          :aria-describedby="describedBy"
          :aria-invalid="invalid"
          data-part="start-month"
          inputmode="numeric"
          @input="capture"
          @blur="commit"
        />
      </template>
    </FormField>
    <FormField :label="copy.endYear">
      <template #default="{ id, describedBy, invalid }">
        <Input
          :id="id"
          v-model="endYear"
          :aria-describedby="describedBy"
          :aria-invalid="invalid"
          data-part="end-year"
          inputmode="numeric"
          :disabled="present"
          @input="capture"
          @blur="commit"
        />
      </template>
    </FormField>
    <FormField :label="copy.endMonth">
      <template #default="{ id, describedBy, invalid }">
        <Input
          :id="id"
          v-model="endMonth"
          :aria-describedby="describedBy"
          :aria-invalid="invalid"
          data-part="end-month"
          inputmode="numeric"
          :disabled="present"
          @input="capture"
          @blur="commit"
        />
      </template>
    </FormField>
    <CheckboxField
      :label="copy.present"
      :model-value="present"
      data-part="present"
      @update:model-value="
        (value) => {
          present = value;
          changePresent();
        }
      "
    />
    <Button
      type="button"
      data-action="unset"
      size="sm"
      variant="ghost"
      @click="unset"
    >
      {{ copy.remove }}
    </Button>
    <p
      v-if="error !== undefined"
      :id="`${fieldId}-error`"
      data-error="date-order"
      role="alert"
    >
      {{ errorCopy }}
    </p>
  </div>
</template>
