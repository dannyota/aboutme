<script setup lang="ts">
import type { LanguageEntry } from '@aboutme/schema';

import type { FieldIntent } from '../fieldIntent';
import TextField from '@/components/app/TextField.vue';
import SelectField from '@/components/app/SelectField.vue';

const levelOptions = [{ value: '', label: 'Not set' }, ...Array.from(
  { length: 6 }, (_, value) => ({ value, label: String(value) }),
)] as const;

defineProps<{ readonly entry: LanguageEntry }>();
const emit = defineEmits<{
  field: [
    change: {
      readonly path: 'name' | 'level';
      readonly intent: FieldIntent<unknown>;
    },
  ];
}>();
function updateLevel(value: string): void {
  emit('field', {
    path: 'level',
    intent: value === ''
      ? { kind: 'unset' }
      : { kind: 'set', value: Number(value) },
  });
}
</script>

<template>
  <TextField
    data-entry-field="name"
    label="Name"
    :model-value="entry.name"
    @intent="emit('field', { path: 'name', intent: $event })"
  />
  <SelectField
    data-entry-field="level"
    label="Level"
    :model-value="entry.level ?? ''"
    :options="levelOptions"
    @update:model-value="updateLevel"
  />
</template>
