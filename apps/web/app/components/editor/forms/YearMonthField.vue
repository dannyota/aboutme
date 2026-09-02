<script setup lang="ts">
import type { YearMonth } from '@aboutme/schema';
import { computed, ref, watch } from 'vue';
import { Button } from '@/components/ui/button';
import FormField from '@/components/app/FormField.vue';
import { Input } from '@/components/ui/input';

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
  if (sameYearMonth(next, props.modelValue)) {
    dirty.value = false;
    return;
  }
  emit('intent', { kind: 'set', value: next });
  dirty.value = false;
}

function sameYearMonth(left: YearMonth, right: YearMonth | undefined): boolean {
  return right !== undefined && left.y === right.y && left.m === right.m;
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
  <div
    role="group"
    :aria-labelledby="`${fieldId}-label`"
    :aria-describedby="describedBy"
  >
    <div
      :id="`${fieldId}-label`"
      class="text-sm font-medium"
    >
      {{ label }}
    </div>
    <FormField label="Year">
      <template #default="{ id, describedBy: partDescribedBy, invalid }">
        <Input
          :id="id"
          v-model="year"
          :aria-describedby="partDescribedBy"
          :aria-invalid="invalid"
          data-part="year"
          inputmode="numeric"
          @input="capture"
          @blur="commit"
        />
      </template>
    </FormField>
    <FormField label="Month">
      <template #default="{ id, describedBy: partDescribedBy, invalid }">
        <Input
          :id="id"
          v-model="month"
          :aria-describedby="partDescribedBy"
          :aria-invalid="invalid"
          data-part="month"
          inputmode="numeric"
          @input="capture"
          @blur="commit"
        />
      </template>
    </FormField>
    <Button
      type="button"
      data-action="unset"
      size="sm"
      variant="ghost"
      @click="unset"
    >
      Remove date
    </Button>
    <p
      v-if="error !== ''"
      :id="`${fieldId}-error`"
      role="alert"
    >
      {{ error }}
    </p>
  </div>
</template>
