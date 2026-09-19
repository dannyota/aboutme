<script setup lang="ts">
import type { CertificateEntry } from '@aboutme/schema';
import { computed } from 'vue';
import { editorFieldsCopy } from '@/i18n/editor-fields';

import type { FieldIntent } from '../fieldIntent';
import TextField from '@/components/app/TextField.vue';
import RichTextEditor from '../../richtext/RichTextEditor.vue';
import YearMonthField from '../YearMonthField.vue';
import EntryLinkField from './EntryLinkField.vue';

defineProps<{ readonly entry: CertificateEntry }>();
const { locale } = useLocale();
const copy = computed(() => editorFieldsCopy[locale.value].entry.certificate);
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
  <EntryLinkField
    data-entry-field="titleLink"
    :label="copy.titleLink"
    :model-value="entry.titleLink"
    @intent="emit('field', { path: 'titleLink', intent: $event })"
  />
  <TextField
    data-entry-field="issuer"
    :label="copy.issuer"
    :model-value="entry.issuer"
    @intent="emit('field', { path: 'issuer', intent: $event })"
  />
  <YearMonthField
    data-entry-field="date"
    :field-id="`${entry.id}-date`"
    :label="copy.date"
    :model-value="entry.date"
    @intent="emit('field', { path: 'date', intent: $event })"
  />
  <RichTextEditor
    data-entry-field="description"
    :label="copy.description"
    :model-value="entry.description ?? ''"
    @update:model-value="updateDescription"
  />
</template>
