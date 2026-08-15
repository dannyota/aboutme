<script setup lang="ts">
import type { EducationEntry } from '@aboutme/schema';

import DateRangeField from '../DateRangeField.vue';
import type { FieldIntent } from '../fieldIntent';
import OptionalField from '../OptionalField.vue';
import RichTextEditor from '../../richtext/RichTextEditor.vue';
import EntryLinkField from './EntryLinkField.vue';

const props = defineProps<{ readonly entry: EducationEntry }>();
const emit = defineEmits<{
  field: [
    change: {
      readonly path:
        | 'degree' | 'school' | 'schoolLink' | 'city' | 'country'
        | 'dates' | 'description';
      readonly intent: FieldIntent<unknown>;
    },
  ];
}>();
function updateDescription(value: string): void {
  emit('field', {
    path: 'description',
    intent: textIntent(props.entry.description, value),
  });
}
function textIntent(
  current: string | undefined,
  value: string,
): FieldIntent<string> {
  return value !== ''
    ? { kind: 'set', value }
    : current === undefined
      ? { kind: 'unset' }
      : { kind: 'clear', value: '' };
}
</script>

<template>
  <OptionalField
    data-entry-field="degree"
    label="Degree"
    :model-value="entry.degree"
    @intent="emit('field', { path: 'degree', intent: $event })"
  />
  <OptionalField
    data-entry-field="school"
    label="School"
    :model-value="entry.school"
    @intent="emit('field', { path: 'school', intent: $event })"
  />
  <EntryLinkField
    data-entry-field="schoolLink"
    label="School link"
    :model-value="entry.schoolLink"
    @intent="emit('field', { path: 'schoolLink', intent: $event })"
  />
  <OptionalField
    data-entry-field="city"
    label="City"
    :model-value="entry.city"
    @intent="emit('field', { path: 'city', intent: $event })"
  />
  <OptionalField
    data-entry-field="country"
    label="Country"
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
    label="Education description"
    :model-value="entry.description ?? ''"
    @update:model-value="updateDescription"
  />
</template>
