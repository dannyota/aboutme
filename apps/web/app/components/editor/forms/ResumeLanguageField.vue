<script setup lang="ts">
/**
 * `ResumeLanguageField` — the resume's content language in the editor.
 * Tiếng Việt and English save on change; Other… saves a BCP 47 tag on blur or
 * Enter once it is valid. "Not set" is offered only while no language is set.
 */
import { computed, ref, watch } from 'vue';
import { editorFieldsCopy } from '@/i18n/editor-fields';

import FormField from '@/components/app/FormField.vue';
import SelectField from '@/components/app/SelectField.vue';
import { Input } from '@/components/ui/input';
import {
  isUnsetLanguage,
  languageChoice,
  OTHER_LANGUAGE,
  parseLanguageTag,
  UNSET_LANGUAGE,
  languageCodeErrorForLocale,
  languageCodeHintForLocale,
  resumeLanguageOptionsForLocale,
  unsetLanguageOptionForLocale,
} from '../resumeLanguage';

const props = defineProps<{ readonly lng: string | null }>();
const emit = defineEmits<{ change: [lng: string] }>();

const choice = ref(languageChoice(props.lng));
const otherText = ref(choice.value === OTHER_LANGUAGE ? (props.lng ?? '') : '');
const error = ref(false);
const { locale } = useLocale();
const copy = computed(() => editorFieldsCopy[locale.value].language);

const options = computed(() =>
  isUnsetLanguage(props.lng)
    ? [
        unsetLanguageOptionForLocale(locale.value),
        ...resumeLanguageOptionsForLocale(locale.value),
      ]
    : [...resumeLanguageOptionsForLocale(locale.value)],
);

watch(
  () => props.lng,
  (lng) => {
    choice.value = languageChoice(lng);
    if (choice.value === OTHER_LANGUAGE) otherText.value = lng ?? '';
    error.value = false;
  },
);

function choose(next: string): void {
  choice.value = next;
  error.value = false;
  if (next === OTHER_LANGUAGE || next === UNSET_LANGUAGE) return;
  if (next !== props.lng) emit('change', next);
}

function commitOther(): void {
  const tag = parseLanguageTag(otherText.value);
  if (tag === null) {
    error.value = true;
    return;
  }
  error.value = false;
  if (tag !== props.lng) emit('change', tag);
}
</script>

<template>
  <div
    class="grid gap-4"
    data-field="lng"
  >
    <SelectField
      :control-attrs="{ 'data-action': 'resume-language' }"
      :hint="copy.hint"
      :label="copy.resumeLanguage"
      :model-value="choice"
      name="lng"
      :options="options"
      @update:model-value="choose"
    />
    <FormField
      v-if="choice === OTHER_LANGUAGE"
      :error="error ? languageCodeErrorForLocale(locale) : undefined"
      :hint="languageCodeHintForLocale(locale)"
      :label="copy.code"
      name="lngOther"
    >
      <template #default="{ id, describedBy, invalid }">
        <Input
          :id="id"
          v-model="otherText"
          :aria-describedby="describedBy"
          :aria-invalid="invalid"
          autocomplete="off"
          maxlength="35"
          name="lngOther"
          @blur="commitOther"
          @keydown.enter.prevent="commitOther"
        />
      </template>
    </FormField>
  </div>
</template>
