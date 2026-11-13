<script setup lang="ts">
import SwitchField from '@/components/app/SwitchField.vue';

defineProps<{
  readonly modelValue: boolean;
  readonly disabled: boolean;
  readonly label: string;
  readonly description: string;
}>();
const emit = defineEmits<{ 'update:modelValue': [value: boolean] }>();
</script>

<template>
  <SwitchField
    :model-value="modelValue"
    :label="label"
    name="signInToView"
    data-action="publish-sign-in-to-view"
    :disabled="disabled"
    :description="description"
    @update:model-value="(value) => emit('update:modelValue', value)"
  />
</template>
