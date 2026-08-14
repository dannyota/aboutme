<script setup lang="ts">
import type { DateRange, YearMonth } from '@aboutme/schema';
import { ref, watch } from 'vue';

import type { FieldIntent } from './fieldIntent';

const props = defineProps<{
  readonly fieldId: string;
  readonly modelValue?: DateRange;
}>();
const emit = defineEmits<{ intent: [intent: FieldIntent<DateRange>] }>();

const dirty = ref(false);
const endMonth = ref(toText(props.modelValue?.end?.m));
const endYear = ref(toText(props.modelValue?.end?.y));
const error = ref('');
const present = ref(props.modelValue?.present ?? false);
const startMonth = ref(toText(props.modelValue?.start.m));
const startYear = ref(toText(props.modelValue?.start.y));

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
  error.value = '';
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
  const start = readYearMonth(startYear.value, startMonth.value);
  if (start === null) {
    if (allEmpty() && props.modelValue !== undefined && !present.value) {
      emit('intent', { kind: 'unset' });
      dirty.value = false;
      return;
    }
    error.value = 'Enter a valid start date.';
    return;
  }
  const end = present.value
    ? null
    : readYearMonth(endYear.value, endMonth.value);
  if (!present.value && end === null) {
    error.value = 'Enter a valid end date or mark this as present.';
    return;
  }
  if (end !== null && compareYearMonth(start, end) > 0) {
    error.value = 'Start date must not be after end date.';
    return;
  }
  emit('intent', {
    kind: 'set',
    value: { start, end, present: present.value },
  });
  dirty.value = false;
}

function unset(): void {
  if (props.modelValue === undefined) return;
  dirty.value = false;
  error.value = '';
  emit('intent', { kind: 'unset' });
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
  const leftMonth = left.m ?? 0;
  const rightMonth = right.m ?? 0;
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
  <fieldset :aria-describedby="error === '' ? undefined : `${fieldId}-error`">
    <legend>Date range</legend>
    <label>
      Start year
      <input
        v-model="startYear"
        data-part="start-year"
        inputmode="numeric"
        @input="capture"
        @blur="commit"
      >
    </label>
    <label>
      Start month
      <input
        v-model="startMonth"
        data-part="start-month"
        inputmode="numeric"
        @input="capture"
        @blur="commit"
      >
    </label>
    <label>
      End year
      <input
        v-model="endYear"
        data-part="end-year"
        inputmode="numeric"
        :disabled="present"
        @input="capture"
        @blur="commit"
      >
    </label>
    <label>
      End month
      <input
        v-model="endMonth"
        data-part="end-month"
        inputmode="numeric"
        :disabled="present"
        @input="capture"
        @blur="commit"
      >
    </label>
    <label>
      <input
        v-model="present"
        type="checkbox"
        data-part="present"
        @change="changePresent"
      >
      Present
    </label>
    <button
      type="button"
      data-action="unset"
      @click="unset"
    >
      Remove date range
    </button>
    <p
      v-if="error !== ''"
      :id="`${fieldId}-error`"
      data-error="date-order"
      role="alert"
    >
      {{ error }}
    </p>
  </fieldset>
</template>
