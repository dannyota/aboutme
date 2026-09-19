<script setup lang="ts">
import type { ProjectEntry } from '@aboutme/schema';
import { computed } from 'vue';
import { editorFieldsCopy } from '@/i18n/editor-fields';

import DateRangeField from '../DateRangeField.vue';
import type { FieldIntent } from '../fieldIntent';
import TextField from '@/components/app/TextField.vue';
import RichTextEditor from '../../richtext/RichTextEditor.vue';
import EntryLinkField from './EntryLinkField.vue';

defineProps<{ readonly entry: ProjectEntry }>();
const { locale } = useLocale();
const copy = computed(() => editorFieldsCopy[locale.value].entry.project);
const emit = defineEmits<{
  field: [
    change: {
      readonly path: 'title' | 'subtitle' | 'link' | 'dates' | 'description';
      readonly intent: FieldIntent<unknown>;
    },
  ];
}>();
function updateDescription(value: string): void {
  emit('field', {
    path: 'description',
    intent: value !== '' ? { kind: 'set', value } : { kind: 'unset' },
  });
}
</script>

<template>
  <TextField
    data-entry-field="title"
    :label="copy.title"
    :model-value="entry.title"
    @intent="emit('field', { path: 'title', intent: $event })"
  />
  <TextField
    data-entry-field="subtitle"
    :label="copy.subtitle"
    :model-value="entry.subtitle"
    @intent="emit('field', { path: 'subtitle', intent: $event })"
  />
  <EntryLinkField
    data-entry-field="link"
    :label="copy.link"
    :model-value="entry.link"
    @intent="emit('field', { path: 'link', intent: $event })"
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
