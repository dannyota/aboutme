<script setup lang="ts">
import type { WorkEntry } from '@aboutme/schema';

import DateRangeField from '../DateRangeField.vue';
import type { FieldIntent } from '../fieldIntent';
import OptionalField from '../OptionalField.vue';
import RichTextEditor from '../../richtext/RichTextEditor.vue';
import EntryLinkField from './EntryLinkField.vue';

const props = defineProps<{ readonly entry: WorkEntry }>();
const emit = defineEmits<{
  field: [
    change: {
      readonly path:
        | 'jobTitle' | 'employer' | 'employerLink' | 'city' | 'country'
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
  if (value !== '') return { kind: 'set', value };
  return current === undefined
    ? { kind: 'unset' }
    : { kind: 'clear', value: '' };
}
</script>

<template>
  <OptionalField
    data-entry-field="jobTitle"
    label="Job title"
    :model-value="entry.jobTitle"
    @intent="emit('field', { path: 'jobTitle', intent: $event })"
  />
  <OptionalField
    data-entry-field="employer"
    label="Employer"
    :model-value="entry.employer"
    @intent="emit('field', { path: 'employer', intent: $event })"
  />
  <EntryLinkField
    data-entry-field="employerLink"
    label="Employer link"
    :model-value="entry.employerLink"
    @intent="emit('field', { path: 'employerLink', intent: $event })"
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
    label="Work description"
    :model-value="entry.description ?? ''"
    @update:model-value="updateDescription"
  />
</template>
