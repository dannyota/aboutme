<script setup lang="ts">
import type { SkillEntry } from '@aboutme/schema';

import type { FieldIntent } from '../fieldIntent';
import TextField from '@/components/app/TextField.vue';
import SelectField from '@/components/app/SelectField.vue';
import RichTextEditor from '../../richtext/RichTextEditor.vue';

const levelOptions = [{ value: '', label: 'Not set' }, ...Array.from(
  { length: 6 }, (_, value) => ({ value, label: String(value) }),
)] as const;

defineProps<{ readonly entry: SkillEntry }>();
const emit = defineEmits<{
  field: [
    change: {
      readonly path: 'name' | 'level' | 'infoHtml';
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
function updateInfo(value: string): void {
  emit('field', {
    path: 'infoHtml',
    intent: value !== ''
      ? { kind: 'set', value }
      : { kind: 'unset' },
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
  <RichTextEditor
    data-entry-field="infoHtml"
    label="Skill information"
    :model-value="entry.infoHtml ?? ''"
    @update:model-value="updateInfo"
  />
</template>
