<script setup lang="ts">
/**
 * `CreateResumeDialog` — create a blank resume, or one started from a gallery
 * sample. The resume language picks the sample language (Vietnamese or
 * English); with another language the English samples show. Starting from a
 * sample copies its content and template into a new private resume.
 */
import type { Resume } from '@aboutme/schema';
import { loadSample, type SampleLanguage } from '@aboutme/schema/samples';
import type { OpaqueCreateOutcome } from '../../../editor/coordinator';
import FormDialog from '@/components/app/FormDialog.vue';
import FormField from '@/components/app/FormField.vue';
import SelectField from '@/components/app/SelectField.vue';
import SheetThumbnail from '@/components/templates/SheetThumbnail.vue';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { GALLERY, sampleRole } from '../../../templates/catalog';
import { suggestedTitle } from '../../../templates/startDocument';
import {
  languageCodeError,
  languageCodeHint,
  OTHER_LANGUAGE,
  parseLanguageTag,
  resumeLanguageOptions,
} from '../resumeLanguage';

const props = defineProps<{
  open: boolean;
  busy: boolean;
  retained: OpaqueCreateOutcome | null;
  /** How many resumes the account has; none opens on the samples. */
  resumeCount?: number;
}>();

const emit = defineEmits<{
  close: [];
  submit: [title: string, lng: string | null | undefined, document?: Resume];
  refresh: [intentId: string];
  abandon: [intentId: string];
}>();

type Mode = 'blank' | 'sample';

// A new resume starts in the site's current language, so it never lands as
// undetermined (`und`); Other takes any BCP 47 tag.
const languageOptions = resumeLanguageOptions;
const samples = GALLERY.filter((template) =>
  template.sampleLanguages.length > 0);

const { locale } = useLocale();
const mode = ref<Mode>('blank');
const title = ref('');
const titleEdited = ref(false);
const languageChoice = ref<string>(locale.value);
const otherLanguage = ref('');
const otherError = ref<string | undefined>();
const selected = ref(samples[0]?.id ?? '');
const documents = ref<Readonly<Record<string, Resume>>>({});
const returnFocus = ref<HTMLElement | null>(null);

const sampleLanguage = computed<SampleLanguage>(() =>
  languageChoice.value === 'vi' || languageChoice.value === 'en'
    ? languageChoice.value
    : 'en');
const chosenDocument = computed(() => documents.value[selected.value]);
const chosenTemplate = computed(() =>
  samples.find((template) => template.id === selected.value));

watch(() => props.open, (open) => {
  if (open) {
    mode.value = props.resumeCount === 0 && samples.length > 0
      ? 'sample'
      : 'blank';
    title.value = '';
    titleEdited.value = false;
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

// Samples load while the dialog is open in sample mode, in the language the
// resume will be written in.
watch([() => props.open, mode, sampleLanguage], async ([
  open,
  current,
  lng,
]) => {
  if (!open || current !== 'sample') return;
  const loaded = await Promise.all(samples.map(async (template) =>
    [template.id, await loadSample(template.id, lng)] as const));
  documents.value = Object.fromEntries(loaded.filter(
    (entry): entry is readonly [string, Resume] => entry[1] !== undefined,
  ));
}, { immediate: true });

// The title follows the chosen sample until the person types their own.
watch([mode, selected, chosenDocument, sampleLanguage], () => {
  if (titleEdited.value) return;
  const template = chosenTemplate.value;
  title.value = mode.value === 'sample' && template
    ? suggestedTitle({ kind: 'sample', template, lng: sampleLanguage.value })
    : '';
});

function editTitle(value: string): void {
  title.value = value;
  titleEdited.value = true;
}

function refresh(): void {
  if (props.retained !== null) emit('refresh', props.retained.intent.id);
}

function abandon(): void {
  if (props.retained !== null) emit('abandon', props.retained.intent.id);
}

function submit(): void {
  let lng: string | null | undefined = languageChoice.value;
  if (languageChoice.value === OTHER_LANGUAGE) {
    const tag = parseLanguageTag(otherLanguage.value);
    if (tag === null) {
      otherError.value = languageCodeError;
      return;
    }
    lng = tag;
  }
  otherError.value = undefined;
  const document = mode.value === 'sample' ? chosenDocument.value : undefined;
  if (mode.value === 'sample') {
    if (document !== undefined) emit('submit', title.value, lng, document);
    return;
  }
  emit('submit', title.value, lng);
}

function persona(templateId: string): string {
  const template = samples.find(({ id }) => id === templateId);
  return template === undefined
    ? ''
    : sampleRole(template, sampleLanguage.value, 'en') ?? '';
}
</script>

<template>
  <FormDialog
    :open="open"
    :class="mode === 'sample' && retained === null
      ? 'create-resume-dialog--samples sm:max-w-[760px]'
      : undefined"
    title="Create resume"
    :description="mode === 'sample'
      ? 'Start from a sample resume and replace its content with yours.'
      : 'Create a new private resume.'"
    :submit-label="mode === 'sample' ? 'Create from sample' : 'Create'"
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
      <div
        aria-label="Start from"
        class="flex w-fit rounded-md border p-0.5"
        role="group"
      >
        <Button
          v-for="option in (['blank', 'sample'] as const)"
          :key="option"
          :aria-pressed="mode === option"
          class="h-8"
          :data-create-mode="option"
          :disabled="busy"
          size="sm"
          type="button"
          :variant="mode === option ? 'default' : 'ghost'"
          @click="mode = option"
        >
          {{ option === 'blank' ? 'Blank' : 'From a sample' }}
        </Button>
      </div>
      <div
        v-if="mode === 'sample'"
        aria-label="Samples"
        class="create-resume-samples"
        role="group"
      >
        <Button
          v-for="template in samples"
          :key="template.id"
          :aria-pressed="selected === template.id"
          class="create-resume-sample h-auto whitespace-normal p-2 text-left"
          :data-sample="template.id"
          :disabled="busy"
          type="button"
          variant="outline"
          @click="selected = template.id"
        >
          <span class="create-resume-sample__sheet">
            <SheetThumbnail
              :document="documents[template.id]"
              :lng="sampleLanguage"
            />
          </span>
          <span class="grid gap-0.5">
            <span class="text-sm font-semibold">{{ template.name }}</span>
            <span class="text-xs font-normal text-muted-foreground">
              {{ persona(template.id) }}
            </span>
          </span>
        </Button>
      </div>
      <FormField
        label="Title"
        name="title"
        required
      >
        <template #default="{ id, describedBy, invalid }">
          <Input
            :id="id"
            :model-value="title"
            :aria-describedby="describedBy"
            :aria-invalid="invalid"
            name="title"
            required
            :disabled="busy"
            @update:model-value="editTitle(String($event))"
          />
        </template>
      </FormField>
      <SelectField
        v-model="languageChoice"
        :control-attrs="{ 'data-action': 'resume-language' }"
        :disabled="busy"
        :hint="mode === 'sample' && languageChoice === OTHER_LANGUAGE
          ? 'Samples are in Vietnamese or English. Your resume language '
            + 'stays the code you enter.'
          : mode === 'sample'
            ? 'Samples come in Vietnamese and English.'
            : 'The language your resume is written in.'"
        label="Resume language"
        name="lng"
        :options="languageOptions"
      />
      <FormField
        v-if="languageChoice === OTHER_LANGUAGE"
        :error="otherError"
        :hint="languageCodeHint"
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
    <template
      v-else
      #footer
    >
      <!-- First here, so it stacks last on phones (DESIGN.md dialogs). -->
      <NuxtLink
        class="create-resume-browse text-sm text-primary underline
          underline-offset-4 sm:mr-auto sm:self-center"
        to="/templates"
      >
        Browse all {{ GALLERY.length }} templates
      </NuxtLink>
      <Button
        :disabled="busy"
        type="button"
        variant="outline"
        data-action="create-cancel"
        @click="emit('close')"
      >
        Cancel
      </Button>
      <Button
        :disabled="busy || (mode === 'sample' && chosenDocument === undefined)"
        type="submit"
        data-action="create-submit"
      >
        {{ mode === 'sample' ? 'Create from sample' : 'Create' }}
      </Button>
    </template>
  </FormDialog>
</template>

<style scoped>
.create-resume-samples {
  display: grid;
  gap: 8px;
}

.create-resume-sample {
  display: grid;
  grid-template-columns: 56px minmax(0, 1fr);
  align-items: center;
  gap: 12px;
}

.create-resume-sample[aria-pressed="true"] {
  border-color: var(--primary);
  box-shadow: 0 0 0 1px var(--primary);
}

.create-resume-sample__sheet {
  display: block;
  width: 56px;
}

/* Wider screens: one row of five cards, thumbnail above the name. */
@media (width >= 640px) {
  .create-resume-samples {
    grid-template-columns: repeat(5, minmax(0, 1fr));
  }

  .create-resume-sample {
    grid-template-columns: minmax(0, 1fr);
    align-content: start;
    align-items: start;
  }

  .create-resume-sample__sheet {
    width: 100%;
  }
}
</style>

<style>
/* Phones: the sample chooser takes the whole screen. */
@media (width < 640px) {
  [data-slot="dialog-content"].create-resume-dialog--samples {
    top: 0;
    left: 0;
    width: 100%;
    max-width: none;
    height: 100dvh;
    overflow-y: auto;
    border-radius: 0;
    transform: none;
    translate: none;
  }
}
</style>
