<script setup lang="ts">
import type { LanguageEntry } from '@aboutme/schema';

import type { FieldIntent } from '../fieldIntent';
import OptionalField from '../OptionalField.vue';

defineProps<{ readonly entry: LanguageEntry }>();
const emit = defineEmits<{
  field: [
    change: {
      readonly path: 'name' | 'level';
      readonly intent: FieldIntent<unknown>;
    },
  ];
}>();
function updateLevel(event: Event): void {
  const value = (event.target as HTMLSelectElement).value;
  emit('field', {
    path: 'level',
    intent: value === ''
      ? { kind: 'unset' }
      : { kind: 'set', value: Number(value) },
  });
}
</script>

<template>
  <OptionalField
    data-entry-field="name"
    label="Name"
    :model-value="entry.name"
    @intent="emit('field', { path: 'name', intent: $event })"
  />
  <label data-entry-field="level">Level
    <select
      :value="entry.level ?? ''"
      @change="updateLevel"
    >
      <option value="">Not set</option>
      <option
        v-for="level in 6"
        :key="level - 1"
        :value="level - 1"
      >
        {{ level - 1 }}
      </option>
    </select></label>
</template>
