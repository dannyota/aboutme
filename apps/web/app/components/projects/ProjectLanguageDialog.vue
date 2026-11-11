<script setup lang="ts">
import { computed, ref, watch } from 'vue';

import FormDialog from '@/components/app/FormDialog.vue';
import SelectField from '@/components/app/SelectField.vue';
import TextField from '@/components/app/TextField.vue';
import type { ProjectsCopy } from '@/i18n/projects';
import { isLanguageKey } from './projectDocument';

const OTHER = 'other';

const props = defineProps<{
  readonly open: boolean;
  readonly existing: readonly string[];
  readonly copy: ProjectsCopy;
}>();
const emit = defineEmits<{ submit: [key: string]; close: [] }>();

const choice = ref(OTHER);
const code = ref('');
const options = computed(() => [
  ...['vi', 'en']
    .filter((key) => !props.existing.includes(key))
    .map((key) => ({ value: key, label: props.copy.languageName(key) })),
  { value: OTHER, label: props.copy.otherLanguage },
]);
const key = computed(() =>
  choice.value === OTHER ? code.value.trim() : choice.value);
const valid = computed(() =>
  isLanguageKey(key.value) && !props.existing.includes(key.value));
const codeError = computed(() =>
  choice.value === OTHER && code.value !== '' && !valid.value
    ? props.copy.languageCodeError
    : undefined);

watch(() => props.open, (open) => {
  if (!open) return;
  choice.value = options.value[0]?.value ?? OTHER;
  code.value = '';
}, { immediate: true });
</script>

<template>
  <FormDialog
    :open="open"
    :title="copy.addLanguageTitle"
    :submit-label="copy.add"
    :cancel-label="copy.cancel"
    :close-label="copy.close"
    :submit-disabled="!valid"
    :restore-focus="false"
    submit-action="project-language-submit"
    @cancel="emit('close')"
    @submit="valid && emit('submit', key)"
  >
    <SelectField
      id="project-language-choice"
      v-model="choice"
      :label="copy.languageLabel"
      :options="options"
      name="language"
    />
    <TextField
      v-if="choice === OTHER"
      id="project-language-code"
      v-model="code"
      :label="copy.languageCode"
      :hint="copy.languageCodeHint"
      :error="codeError"
      autocomplete="off"
      name="code"
    />
  </FormDialog>
</template>
