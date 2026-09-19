<script setup lang="ts">
import type { OpaqueCreateOutcome } from '../../../editor/coordinator';
import FormDialog from '@/components/app/FormDialog.vue';
import FormField from '@/components/app/FormField.vue';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import SelectField from '@/components/app/SelectField.vue';

const props = defineProps<{
  open: boolean;
  busy: boolean;
  retained: OpaqueCreateOutcome | null;
}>();

const emit = defineEmits<{
  close: [];
  submit: [title: string, lng: string | null | undefined];
  refresh: [intentId: string];
  abandon: [intentId: string];
}>();

// A new resume starts in the site's current language, so it never lands as
// undetermined (`und`); Other takes any BCP 47 tag.
const languageOptions = [
  { value: 'vi', label: 'Tiếng Việt' },
  { value: 'en', label: 'English' },
  { value: 'other', label: 'Other…' },
] as const;
const BCP47 = /^[A-Za-z]{2,8}(?:-[A-Za-z0-9]{1,8})*$/;

const { locale } = useLocale();
const title = ref('');
const languageChoice = ref<string>(locale.value);
const otherLanguage = ref('');
const otherError = ref<string | undefined>();
const returnFocus = ref<HTMLElement | null>(null);

watch(() => props.open, (open) => {
  if (open) {
    languageChoice.value = locale.value;
    otherLanguage.value = '';
    otherError.value = undefined;
    returnFocus.value = document.activeElement instanceof HTMLElement
      ? document.activeElement
      : null;
  } else if (returnFocus.value !== null) {
    const target = returnFocus.value;
    returnFocus.value = null;
    void nextTick(() => target.focus());
  }
}, { immediate: true });

function refresh(): void {
  if (props.retained !== null) emit('refresh', props.retained.intent.id);
}

function abandon(): void {
  if (props.retained !== null) emit('abandon', props.retained.intent.id);
}

function submit(): void {
  if (languageChoice.value !== 'other') {
    emit('submit', title.value, languageChoice.value);
    return;
  }
  const tag = otherLanguage.value.trim();
  if (tag.length > 35 || !BCP47.test(tag)) {
    otherError.value = 'Enter a language code, such as fr or zh-Hant.';
    return;
  }
  otherError.value = undefined;
  emit('submit', title.value, tag);
}
</script>

<template>
  <FormDialog
    :open="open"
    title="Create resume"
    description="Create a new private resume."
    submit-label="Create"
    :busy="busy"
    @cancel="emit('close')"
    @submit="submit"
  >
    <template v-if="retained !== null">
      <p role="alert">
        We could not confirm whether this resume was created.
      </p>
    </template>
    <template v-else>
      <FormField
        label="Title"
        name="title"
        required
      >
        <template #default="{ id, describedBy, invalid }">
          <Input
            :id="id"
            v-model="title"
            :aria-describedby="describedBy"
            :aria-invalid="invalid"
            name="title"
            required
            :disabled="busy"
          />
        </template>
      </FormField>
      <SelectField
        v-model="languageChoice"
        :control-attrs="{ 'data-action': 'resume-language' }"
        :disabled="busy"
        hint="The language your resume is written in."
        label="Resume language"
        name="lng"
        :options="languageOptions"
      />
      <FormField
        v-if="languageChoice === 'other'"
        :error="otherError"
        hint="A BCP 47 tag, such as fr or zh-Hant."
        label="Language code"
        name="lngOther"
      >
        <template #default="{ id, describedBy, invalid }">
          <Input
            :id="id"
            v-model="otherLanguage"
            :aria-describedby="describedBy"
            :aria-invalid="invalid"
            autocomplete="off"
            maxlength="35"
            name="lngOther"
            :disabled="busy"
          />
        </template>
      </FormField>
    </template>
    <template
      v-if="retained !== null"
      #footer
    >
      <Button
        :disabled="busy"
        type="button"
        variant="outline"
        @click="refresh"
      >
        Refresh list
      </Button>
      <Button
        :disabled="busy"
        type="button"
        @click="abandon"
      >
        Abandon
      </Button>
    </template>
  </FormDialog>
</template>
