<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue';
import type { FieldIntent } from '@/components/editor/forms/fieldIntent';
import { Input } from '@/components/ui/input';
import { Textarea } from '@/components/ui/textarea';
import { cn } from '@/lib/utils';
import FormField from './FormField.vue';

const props = withDefaults(
  defineProps<{
    readonly label: string;
    readonly modelValue?: string;
    readonly id?: string;
    readonly name?: string;
    readonly type?: 'text' | 'email' | 'url';
    readonly multiline?: boolean;
    readonly rows?: number;
    readonly autocomplete?: string;
    readonly inputmode?: string;
    readonly placeholder?: string;
    readonly hint?: string;
    readonly error?: string;
    readonly required?: boolean;
    readonly disabled?: boolean;
    readonly controlAttrs?: Record<string, string>;
    readonly class?: string;
  }>(),
  { type: 'text', rows: 3 },
);
const emit = defineEmits<{ intent: [intent: FieldIntent<string>] }>();
const draft = ref(props.modelValue ?? '');
const dirty = ref(false);
const mounted = ref(true);
const control = ref<{ $el?: HTMLElement } | null>(null);

watch(
  () => props.modelValue,
  (next) => {
    if (!dirty.value) draft.value = next ?? '';
  },
);
function onInput(value: string | number): void {
  draft.value = String(value);
  dirty.value = draft.value !== (props.modelValue ?? '');
}
function commit(): void {
  if (!mounted.value || !dirty.value) return;
  const value = draft.value;
  dirty.value = false;
  if (value === '') {
    if (props.modelValue !== undefined) emit('intent', { kind: 'unset' });
  } else if (value !== props.modelValue) {
    emit('intent', { kind: 'set', value });
  }
}
function onKeydown(event: KeyboardEvent): void {
  if (event.key === 'Escape') {
    event.preventDefault();
    draft.value = props.modelValue ?? '';
    dirty.value = false;
  } else if (event.key === 'Enter' && !props.multiline) {
    event.preventDefault();
    commit();
  }
}
onBeforeUnmount(() => {
  mounted.value = false;
});
defineExpose({ focus: (): void => control.value?.$el?.focus() });
</script>

<template>
  <FormField
    :id="id"
    v-slot="{ id: fieldId, describedBy, invalid }"
    :class="cn(props.class)"
    :error="error"
    :hint="hint"
    :label="label"
    :name="name"
    :required="required"
  >
    <component
      :is="multiline ? Textarea : Input"
      :id="fieldId"
      ref="control"
      :aria-describedby="describedBy"
      :aria-invalid="invalid"
      :autocomplete="autocomplete"
      v-bind="controlAttrs"
      data-field-input
      :disabled="disabled"
      :inputmode="inputmode"
      :model-value="draft"
      :placeholder="placeholder"
      :required="required"
      :rows="multiline ? rows : undefined"
      :type="multiline ? undefined : type"
      @blur="commit"
      @keydown="onKeydown"
      @update:model-value="onInput"
    />
  </FormField>
</template>
