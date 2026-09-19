<script setup lang="ts">
import type { ProfileEntry } from '@aboutme/schema';

import RichTextEditor from '../../richtext/RichTextEditor.vue';
import { computed } from 'vue';
import { editorFieldsCopy } from '@/i18n/editor-fields';
import type { FieldIntent } from '../fieldIntent';

defineProps<{ readonly entry: ProfileEntry }>();
const emit = defineEmits<{
  field: [
    change: { readonly path: 'text'; readonly intent: FieldIntent<string> },
  ];
}>();
const { locale } = useLocale();
const copy = computed(() => editorFieldsCopy[locale.value].entry.profile);

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
    :label="copy.text"
    :model-value="entry.text ?? ''"
    @update:model-value="updateText"
  />
</template>
