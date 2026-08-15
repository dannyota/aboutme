<script setup lang="ts">
import { computed, ref, watch } from 'vue';

const props = withDefaults(
  defineProps<{
    readonly fallback?: string;
    readonly fieldId?: string;
    readonly label: string;
    readonly modelValue?: string;
    readonly required?: boolean;
    readonly unsetAction?: string;
  }>(),
  { fallback: '', fieldId: 'color', required: false, unsetAction: 'unset' },
);

const emit = defineEmits<{
  set: [value: string];
  unset: [];
}>();

const dirty = ref(false);
const error = ref('');
const removeButton = ref<HTMLButtonElement | null>(null);
const value = ref(props.modelValue ?? props.fallback);
const inputId = computed(
  () => `customization-${props.fieldId.replaceAll('.', '-')}`,
);
const errorId = computed(() => `${inputId.value}-error`);

watch(
  () => [props.modelValue, props.fallback] as const,
  ([modelValue, fallback]) => {
    if (!dirty.value) value.value = modelValue ?? fallback;
  },
);

function capture(): void {
  dirty.value = true;
  error.value = '';
}

function commit(event: FocusEvent): void {
  if (removeButton.value !== null
    && event.relatedTarget === removeButton.value) {
    return;
  }
  if (!dirty.value) return;
  if (!isHexColor(value.value)) {
    error.value = 'Enter a six-digit hex color.';
    return;
  }
  if (value.value !== (props.modelValue ?? props.fallback)) {
    emit('set', value.value);
  }
  dirty.value = false;
}

function prepareRemove(event: Event): void {
  event.preventDefault();
}

function remove(): void {
  if (props.modelValue === undefined) return;
  emit('unset');
  dirty.value = false;
  error.value = '';
}

function isHexColor(candidate: string): boolean {
  return /^#[0-9a-fA-F]{6}$/.test(candidate);
}
</script>

<template>
  <label :for="inputId">
    {{ label }}
    <input
      :id="inputId"
      v-model="value"
      type="text"
      inputmode="text"
      :aria-invalid="error === '' ? undefined : 'true'"
      :aria-describedby="error === '' ? undefined : errorId"
      @input="capture"
      @blur="commit"
    >
  </label>
  <p
    v-if="error !== ''"
    :id="errorId"
    :data-error-for="fieldId"
    role="alert"
  >
    {{ error }}
  </p>
  <button
    v-if="!required"
    ref="removeButton"
    type="button"
    :data-action="unsetAction"
    @pointerdown="prepareRemove"
    @mousedown="prepareRemove"
    @click="remove"
  >
    Remove
  </button>
</template>
