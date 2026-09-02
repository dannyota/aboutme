<script setup lang="ts">
import {
  NativeSelect,
  NativeSelectOption,
} from '@/components/ui/native-select';
import { cn } from '@/lib/utils';
import FormField from './FormField.vue';

const props = defineProps<{
  readonly label: string;
  readonly modelValue: string | number;
  readonly options: readonly { value: string | number; label: string }[];
  readonly id?: string;
  readonly name?: string;
  readonly hint?: string;
  readonly error?: string;
  readonly disabled?: boolean;
  readonly controlAttrs?: Record<string, string>;
  readonly class?: string;
}>();
const emit = defineEmits<{ 'update:modelValue': [value: string] }>();

function onChange(event: Event): void {
  emit('update:modelValue', (event.target as HTMLSelectElement).value);
}
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
  >
    <NativeSelect
      :id="fieldId"
      :model-value="modelValue"
      v-bind="controlAttrs"
      data-field-input
      :aria-describedby="describedBy"
      :aria-invalid="invalid"
      :disabled="disabled"
      @change="onChange"
    >
      <NativeSelectOption
        v-for="option in options"
        :key="String(option.value)"
        :value="option.value"
      >
        {{ option.label }}
      </NativeSelectOption>
    </NativeSelect>
  </FormField>
</template>
