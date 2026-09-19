<script setup lang="ts">
import type { SkillEntry } from '@aboutme/schema';

import type { FieldIntent } from '../fieldIntent';
import { computed } from 'vue';
import { editorFieldsCopy } from '@/i18n/editor-fields';
import TextField from '@/components/app/TextField.vue';
import SelectField from '@/components/app/SelectField.vue';
import { skillLevelOptions } from './levels';
import RichTextEditor from '../../richtext/RichTextEditor.vue';

const { locale } = useLocale();
const copy = computed(() => editorFieldsCopy[locale.value].entry.skill);
const levelOptions = computed(() => skillLevelOptions(locale.value));

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
    intent:
      value === '' ? { kind: 'unset' } : { kind: 'set', value: Number(value) },
  });
}
function updateInfo(value: string): void {
  emit('field', {
    path: 'infoHtml',
    intent: value !== '' ? { kind: 'set', value } : { kind: 'unset' },
  });
}
</script>

<template>
  <TextField
    data-entry-field="name"
    :label="copy.name"
    :model-value="entry.name"
    @intent="emit('field', { path: 'name', intent: $event })"
  />
  <SelectField
    data-entry-field="level"
    :label="copy.level"
    :model-value="entry.level ?? ''"
    :options="levelOptions"
    @update:model-value="updateLevel"
  />
  <RichTextEditor
    data-entry-field="infoHtml"
    :label="copy.infoHtml"
    :model-value="entry.infoHtml ?? ''"
    @update:model-value="updateInfo"
  />
</template>
