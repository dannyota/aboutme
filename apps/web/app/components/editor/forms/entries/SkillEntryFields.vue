<script setup lang="ts">
import type { SkillEntry } from '@aboutme/schema';

import type { FieldIntent } from '../fieldIntent';
import OptionalField from '../OptionalField.vue';
import RichTextEditor from '../../richtext/RichTextEditor.vue';

const props = defineProps<{ readonly entry: SkillEntry }>();
const emit = defineEmits<{
  field: [
    change: {
      readonly path: 'name' | 'level' | 'infoHtml';
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
function updateInfo(value: string): void {
  emit('field', {
    path: 'infoHtml',
    intent: value !== ''
      ? { kind: 'set', value }
      : props.entry.infoHtml === undefined
        ? { kind: 'unset' }
        : { kind: 'clear', value: '' },
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
  <RichTextEditor
    data-entry-field="infoHtml"
    label="Skill information"
    :model-value="entry.infoHtml ?? ''"
    @update:model-value="updateInfo"
  />
</template>
