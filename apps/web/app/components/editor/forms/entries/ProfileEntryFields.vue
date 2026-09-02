<script setup lang="ts">
import type { ProfileEntry } from '@aboutme/schema';

import RichTextEditor from '../../richtext/RichTextEditor.vue';
import type { FieldIntent } from '../fieldIntent';

defineProps<{ readonly entry: ProfileEntry }>();
const emit = defineEmits<{
  field: [
    change: { readonly path: 'text'; readonly intent: FieldIntent<string> },
  ];
}>();

function updateText(value: string): void {
  emit('field', { path: 'text', intent: textIntent(value) });
}

function textIntent(value: string): FieldIntent<string> {
  if (value !== '') return { kind: 'set', value };
  return { kind: 'unset' };
}
</script>

<template>
  <RichTextEditor
    data-entry-field="text"
    label="Profile text"
    :model-value="entry.text ?? ''"
    @update:model-value="updateText"
  />
</template>
