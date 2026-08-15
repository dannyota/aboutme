<script setup lang="ts">
import type { CertificateEntry } from '@aboutme/schema';

import type { FieldIntent } from '../fieldIntent';
import OptionalField from '../OptionalField.vue';
import RichTextEditor from '../../richtext/RichTextEditor.vue';
import YearMonthField from '../YearMonthField.vue';
import EntryLinkField from './EntryLinkField.vue';

const props = defineProps<{ readonly entry: CertificateEntry }>();
const emit = defineEmits<{
  field: [
    change: {
      readonly path: 'title' | 'titleLink' | 'issuer' | 'date' | 'description';
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
    data-entry-field="titleLink"
    label="Title link"
    :model-value="entry.titleLink"
    @intent="emit('field', { path: 'titleLink', intent: $event })"
  />
  <OptionalField
    data-entry-field="issuer"
    label="Issuer"
    :model-value="entry.issuer"
    @intent="emit('field', { path: 'issuer', intent: $event })"
  />
  <YearMonthField
    data-entry-field="date"
    :field-id="`${entry.id}-date`"
    label="Date"
    :model-value="entry.date"
    @intent="emit('field', { path: 'date', intent: $event })"
  />
  <RichTextEditor
    data-entry-field="description"
    label="Certificate description"
    :model-value="entry.description ?? ''"
    @update:model-value="updateDescription"
  />
</template>
