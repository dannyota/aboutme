<script setup lang="ts">
import type { EducationEntry } from '@aboutme/schema';
import { computed } from 'vue';
import { editorFieldsCopy } from '@/i18n/editor-fields';

import DateRangeField from '../DateRangeField.vue';
import type { FieldIntent } from '../fieldIntent';
import TextField from '@/components/app/TextField.vue';
import RichTextEditor from '../../richtext/RichTextEditor.vue';
import EntryLinkField from './EntryLinkField.vue';

defineProps<{ readonly entry: EducationEntry }>();
const { locale } = useLocale();
const copy = computed(() => editorFieldsCopy[locale.value].entry.education);
const emit = defineEmits<{
  field: [
    change: {
      readonly path:
        | 'degree'
        | 'school'
        | 'schoolLink'
        | 'city'
        | 'country'
        | 'dates'
        | 'description';
      readonly intent: FieldIntent<unknown>;
    },
  ];
}>();
function updateDescription(value: string): void {
  emit('field', {
    path: 'description',
    intent: textIntent(value),
  });
}
function textIntent(value: string): FieldIntent<string> {
  return value === '' ? { kind: 'unset' } : { kind: 'set', value };
}
</script>

<template>
  <TextField
    data-entry-field="degree"
    :label="copy.degree"
    :model-value="entry.degree"
    @intent="emit('field', { path: 'degree', intent: $event })"
  />
  <TextField
    data-entry-field="school"
    :label="copy.school"
    :model-value="entry.school"
    @intent="emit('field', { path: 'school', intent: $event })"
  />
  <EntryLinkField
    data-entry-field="schoolLink"
    :label="copy.schoolLink"
    :model-value="entry.schoolLink"
    @intent="emit('field', { path: 'schoolLink', intent: $event })"
  />
  <TextField
    data-entry-field="city"
    :label="copy.city"
    :model-value="entry.city"
    @intent="emit('field', { path: 'city', intent: $event })"
  />
  <TextField
    data-entry-field="country"
    :label="copy.country"
    :model-value="entry.country"
    @intent="emit('field', { path: 'country', intent: $event })"
  />
  <DateRangeField
    data-entry-field="dates"
    :field-id="`${entry.id}-dates`"
    :model-value="entry.dates"
    @intent="emit('field', { path: 'dates', intent: $event })"
  />
  <RichTextEditor
    data-entry-field="description"
    :label="copy.description"
    :model-value="entry.description ?? ''"
    @update:model-value="updateDescription"
  />
</template>
