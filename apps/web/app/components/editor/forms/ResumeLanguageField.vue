<script setup lang="ts">
/**
 * `ResumeLanguageField` — the resume's content language in the editor.
 * Tiếng Việt and English save on change; Other… saves a BCP 47 tag on blur or
 * Enter once it is valid. "Not set" is offered only while no language is set.
 */
import { computed, ref, watch } from 'vue';

import FormField from '@/components/app/FormField.vue';
import SelectField from '@/components/app/SelectField.vue';
import { Input } from '@/components/ui/input';
import {
  isUnsetLanguage,
  languageChoice,
  languageCodeError,
  languageCodeHint,
  OTHER_LANGUAGE,
  parseLanguageTag,
  resumeLanguageOptions,
  UNSET_LANGUAGE,
  unsetLanguageOption,
} from '../resumeLanguage';

const props = defineProps<{ readonly lng: string | null }>();
const emit = defineEmits<{ change: [lng: string] }>();

const choice = ref(languageChoice(props.lng));
const otherText = ref(choice.value === OTHER_LANGUAGE ? props.lng ?? '' : '');
const error = ref<string | undefined>();

const options = computed(() =>
  isUnsetLanguage(props.lng)
    ? [unsetLanguageOption, ...resumeLanguageOptions]
    : [...resumeLanguageOptions]);

watch(() => props.lng, (lng) => {
  choice.value = languageChoice(lng);
  if (choice.value === OTHER_LANGUAGE) otherText.value = lng ?? '';
  error.value = undefined;
});

function choose(next: string): void {
  choice.value = next;
  error.value = undefined;
  if (next === OTHER_LANGUAGE || next === UNSET_LANGUAGE) return;
  if (next !== props.lng) emit('change', next);
}

function commitOther(): void {
  const tag = parseLanguageTag(otherText.value);
  if (tag === null) {
    error.value = languageCodeError;
    return;
  }
  error.value = undefined;
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
      hint="The language your resume is written in."
      label="Resume language"
      :model-value="choice"
      name="lng"
      :options="options"
      @update:model-value="choose"
    />
    <FormField
      v-if="choice === OTHER_LANGUAGE"
      :error="error"
      :hint="languageCodeHint"
      label="Language code"
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
