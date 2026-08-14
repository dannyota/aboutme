<script setup lang="ts">
import { ref, watch } from 'vue';

import type { FieldIntent } from './fieldIntent';

const props = withDefaults(
  defineProps<{
    readonly clearable?: boolean;
    readonly label: string;
    readonly modelValue?: string;
  }>(),
  { clearable: true },
);

const emit = defineEmits<{ intent: [intent: FieldIntent<string>] }>();

const dirty = ref(false);
const pending = ref<FieldIntent<string> | null>(null);
const value = ref(props.modelValue ?? '');

watch(
  () => props.modelValue,
  (next) => {
    if (!dirty.value) value.value = next ?? '';
  },
);

function capture(): void {
  dirty.value = true;
  if (value.value === '') {
    pending.value
      = props.modelValue === undefined
        ? { kind: 'unset' }
        : { kind: 'clear', value: '' };
    return;
  }
  pending.value = { kind: 'set', value: value.value };
}

function choose(intent: FieldIntent<string>): void {
  if (intent.kind === 'clear' || intent.kind === 'unset') value.value = '';
  if (props.modelValue === undefined && intent.kind === 'unset') return;
  emit('intent', intent);
  dirty.value = false;
  pending.value = null;
}

function set(): void {
  capture();
  commit();
}

function commit(): void {
  if (!dirty.value || pending.value === null) return;
  if (props.modelValue === undefined && pending.value.kind === 'unset') {
    dirty.value = false;
    pending.value = null;
    return;
  }
  emit('intent', pending.value);
  dirty.value = false;
  pending.value = null;
}
</script>

<template>
  <div>
    <label>
      {{ label }}
      <input
        v-model="value"
        @input="capture"
        @blur="commit"
      >
    </label>
    <button
      type="button"
      data-action="set"
      @mousedown.prevent
      @click="set"
    >
      Set
    </button>
    <button
      v-if="clearable"
      type="button"
      data-action="clear"
      @mousedown.prevent
      @click="choose({ kind: 'clear', value: '' })"
    >
      Clear
    </button>
    <button
      type="button"
      data-action="unset"
      @mousedown.prevent
      @click="choose({ kind: 'unset' })"
    >
      Remove
    </button>
  </div>
</template>
