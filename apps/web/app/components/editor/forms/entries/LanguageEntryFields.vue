<script setup lang="ts">
import type { LanguageEntry } from '@aboutme/schema';

import type { FieldIntent } from '../fieldIntent';
import { computed } from 'vue';
import { editorFieldsCopy } from '@/i18n/editor-fields';
import TextField from '@/components/app/TextField.vue';
import SelectField from '@/components/app/SelectField.vue';
import { languageLevelOptions } from './levels';

const { locale } = useLocale();
const copy = computed(() => editorFieldsCopy[locale.value].entry.language);
const levelOptions = computed(() => languageLevelOptions(locale.value));

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
    intent:
      value === '' ? { kind: 'unset' } : { kind: 'set', value: Number(value) },
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
</template>
