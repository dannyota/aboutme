<script setup lang="ts">
import type { WorkEntry } from '@aboutme/schema';

import DateRangeField from '../DateRangeField.vue';
import type { FieldIntent } from '../fieldIntent';
import TextField from '@/components/app/TextField.vue';
import RichTextEditor from '../../richtext/RichTextEditor.vue';
import EntryLinkField from './EntryLinkField.vue';

defineProps<{ readonly entry: WorkEntry }>();
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
    intent: textIntent(value),
  });
}

function textIntent(value: string): FieldIntent<string> {
  if (value !== '') return { kind: 'set', value };
  return { kind: 'unset' };
}
</script>

<template>
  <TextField
    data-entry-field="jobTitle"
    label="Job title"
    :model-value="entry.jobTitle"
    @intent="emit('field', { path: 'jobTitle', intent: $event })"
  />
  <TextField
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
  <TextField
    data-entry-field="city"
    label="City"
    :model-value="entry.city"
    @intent="emit('field', { path: 'city', intent: $event })"
  />
  <TextField
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
