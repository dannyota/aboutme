<script setup lang="ts">
import type { ProfileEntry } from '@aboutme/schema';

import RichTextEditor from '../../richtext/RichTextEditor.vue';
import type { FieldIntent } from '../fieldIntent';

const props = defineProps<{ readonly entry: ProfileEntry }>();
const emit = defineEmits<{
  field: [
    change: { readonly path: 'text'; readonly intent: FieldIntent<string> },
  ];
}>();

function updateText(value: string): void {
  emit('field', { path: 'text', intent: textIntent(props.entry.text, value) });
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
  <RichTextEditor
    data-entry-field="text"
    label="Profile text"
    :model-value="entry.text ?? ''"
    @update:model-value="updateText"
  />
</template>
