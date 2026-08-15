<script setup lang="ts">
import type { ProjectEntry } from '@aboutme/schema';

import DateRangeField from '../DateRangeField.vue';
import type { FieldIntent } from '../fieldIntent';
import OptionalField from '../OptionalField.vue';
import RichTextEditor from '../../richtext/RichTextEditor.vue';
import EntryLinkField from './EntryLinkField.vue';

const props = defineProps<{ readonly entry: ProjectEntry }>();
const emit = defineEmits<{
  field: [
    change: {
      readonly path: 'title' | 'link' | 'dates' | 'description';
      readonly intent: FieldIntent<unknown>;
    },
  ];
}>();
function updateDescription(value: string): void {
  emit('field', {
    path: 'description',
    intent: value !== ''
      ? { kind: 'set', value }
      : props.entry.description === undefined
        ? { kind: 'unset' }
        : { kind: 'clear', value: '' },
  });
}
</script>

<template>
  <OptionalField
    data-entry-field="title"
    label="Title"
    :model-value="entry.title"
    @intent="emit('field', { path: 'title', intent: $event })"
  />
  <EntryLinkField
    data-entry-field="link"
    label="Link"
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
    label="Project description"
    :model-value="entry.description ?? ''"
    @update:model-value="updateDescription"
  />
</template>
