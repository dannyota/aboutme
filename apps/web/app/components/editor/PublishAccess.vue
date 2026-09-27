<script setup lang="ts">
import SwitchField from '@/components/app/SwitchField.vue';

// Holds only the "Require sign-in to view" switch, shown in PublishDialog
// when the capability is open or the resume already has it on
// (docs/design/viewer-analytics/sign-in-to-view.md#setting).
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
