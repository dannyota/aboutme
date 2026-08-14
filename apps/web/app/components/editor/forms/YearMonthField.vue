<script setup lang="ts">
import type { YearMonth } from '@aboutme/schema';
import { computed, ref, watch } from 'vue';

import type { FieldIntent } from './fieldIntent';

const props = defineProps<{
  readonly fieldId: string;
  readonly label: string;
  readonly modelValue?: YearMonth;
}>();

const emit = defineEmits<{ intent: [intent: FieldIntent<YearMonth>] }>();

const dirty = ref(false);
const month = ref(toText(props.modelValue?.m));
const year = ref(toText(props.modelValue?.y));
const error = ref('');

watch(
  () => props.modelValue,
  (next) => {
    if (!dirty.value) {
      year.value = toText(next?.y);
      month.value = toText(next?.m);
    }
  },
);

const describedBy = computed(() =>
  error.value === '' ? undefined : `${props.fieldId}-error`,
);

function capture(): void {
  dirty.value = true;
  error.value = '';
}

function commit(): void {
  if (!dirty.value) return;
  const next = readYearMonth(year.value, month.value);
  if (next === null) {
    if (year.value === '' && month.value === '') {
      if (props.modelValue !== undefined) emit('intent', { kind: 'unset' });
      dirty.value = false;
      return;
    }
    error.value = 'Enter a valid year and month.';
    return;
  }
  emit('intent', { kind: 'set', value: next });
  dirty.value = false;
}

function unset(): void {
  if (props.modelValue === undefined) return;
  dirty.value = false;
  year.value = '';
  month.value = '';
  error.value = '';
  emit('intent', { kind: 'unset' });
}

function readYearMonth(yearText: string, monthText: string): YearMonth | null {
  const parsedYear = readInteger(yearText, 1900, 2100);
  if (parsedYear === null) return null;
  if (monthText === '') return { y: parsedYear };
  const parsedMonth = readInteger(monthText, 1, 12);
  return parsedMonth === null ? null : { y: parsedYear, m: parsedMonth };
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
  <fieldset :aria-describedby="describedBy">
    <legend>{{ label }}</legend>
    <label>
      Year
      <input
        v-model="year"
        data-part="year"
        inputmode="numeric"
        @input="capture"
        @blur="commit"
      >
    </label>
    <label>
      Month
      <input
        v-model="month"
        data-part="month"
        inputmode="numeric"
        @input="capture"
        @blur="commit"
      >
    </label>
    <button
      type="button"
      data-action="unset"
      @click="unset"
    >
      Remove date
    </button>
    <p
      v-if="error !== ''"
      :id="`${fieldId}-error`"
      role="alert"
    >
      {{ error }}
    </p>
  </fieldset>
</template>
