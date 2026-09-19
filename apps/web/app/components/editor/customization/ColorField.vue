<script setup lang="ts">
import { computed, ref, watch } from 'vue';

import FormField from '../../app/FormField.vue';
import { Button } from '../../ui/button';
import { Input } from '../../ui/input';
import { editorControlsCopy } from '../../../i18n/editor-controls';

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
const { locale } = useLocale();
const copy = computed(() => editorControlsCopy[locale.value].controls);

const dirty = ref(false);
const invalidColor = ref(false);
const error = computed(() =>
  invalidColor.value ? copy.value.colorInvalid : '',
);
const removeButton = ref<HTMLButtonElement | null>(null);
const value = ref(props.modelValue ?? props.fallback);
const inputId = computed(
  () => `customization-${props.fieldId.replaceAll('.', '-')}`,
);

watch(
  () => [props.modelValue, props.fallback] as const,
  ([modelValue, fallback]) => {
    if (!dirty.value) value.value = modelValue ?? fallback;
  },
);

function capture(): void {
  dirty.value = true;
  invalidColor.value = false;
}

function commit(event: FocusEvent): void {
  const related = event.relatedTarget;
  if (related instanceof HTMLElement
    && related.dataset.action === props.unsetAction) {
    return;
  }
  if (!dirty.value) return;
  if (!isHexColor(value.value)) {
    invalidColor.value = true;
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
  invalidColor.value = false;
}

function isHexColor(candidate: string): boolean {
  return /^#[0-9a-fA-F]{6}$/.test(candidate);
}
</script>

<template>
  <FormField
    :id="inputId"
    :error="error || undefined"
    :label="label"
    :name="fieldId"
  >
    <template #default="{ id, describedBy, invalid }">
      <div class="flex items-center gap-2">
        <Input
          :id="id"
          v-model="value"
          :aria-describedby="describedBy"
          :aria-invalid="invalid"
          inputmode="text"
          spellcheck="false"
          type="text"
          @input="capture"
          @blur="commit"
        />
        <span
          class="size-6 rounded border"
          :style="{ backgroundColor: isHexColor(value) ? value : undefined }"
          aria-hidden="true"
        />
      </div>
    </template>
  </FormField>
  <Button
    v-if="!required"
    ref="removeButton"
    type="button"
    variant="ghost"
    size="sm"
    :data-action="unsetAction"
    @pointerdown="prepareRemove"
    @mousedown="prepareRemove"
    @click="remove"
  >
    {{ copy.colorRemove }}
  </Button>
</template>
