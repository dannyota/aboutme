<script setup lang="ts">
import TextField from '@/components/app/TextField.vue';
import RichTextEditor from '@/components/editor/richtext/RichTextEditor.vue';
import type { ProjectsCopy } from '@/i18n/projects';
import {
  characterCount,
  COUNTER_WINDOW,
  PLAIN_TEXT_FIELDS,
  TEXT_LIMITS,
  type PlainTextField,
  type ProjectTextDraft,
} from './projectDocument';

const props = defineProps<{
  readonly language: string;
  readonly text: ProjectTextDraft;
  readonly isDefault: boolean;
  readonly errors: Readonly<Record<string, string>>;
  readonly copy: ProjectsCopy;
}>();
const emit = defineEmits<{
  update: [field: keyof ProjectTextDraft, value: string];
}>();

function counter(field: PlainTextField): string | undefined {
  const count = characterCount(props.text[field]);
  const limit = TEXT_LIMITS[field];
  return count >= limit - COUNTER_WINDOW
    ? props.copy.counter(count, limit)
    : undefined;
}

function fieldId(field: keyof ProjectTextDraft): string {
  return `project-${props.language}-${field}`;
}
</script>

<template>
  <div class="grid gap-4">
    <TextField
      v-for="field in PLAIN_TEXT_FIELDS"
      :id="fieldId(field)"
      :key="field"
      :model-value="text[field]"
      :label="copy.fields[field]"
      :hint="counter(field)"
      :error="errors[`text.${language}.${field}`]"
      :multiline="field === 'summary' || field === 'outcome'"
      :required="field === 'name' && isDefault"
      :name="field"
      autocomplete="off"
      @update:model-value="(value) => emit('update', field, value)"
    />
    <div
      :id="fieldId('description')"
      class="grid gap-1.5"
      data-field="description"
    >
      <p class="text-sm font-medium">
        {{ copy.fields.description }}
      </p>
      <RichTextEditor
        :label="copy.fields.description"
        :model-value="text.description"
        @update:model-value="(value) => emit('update', 'description', value)"
      />
      <p
        v-if="errors[`text.${language}.description`]"
        role="alert"
        class="text-xs text-destructive"
      >
        {{ errors[`text.${language}.description`] }}
      </p>
    </div>
  </div>
</template>
