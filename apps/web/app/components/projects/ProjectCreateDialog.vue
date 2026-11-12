<script setup lang="ts">
import { computed, ref, watch } from 'vue';

import CheckboxField from '@/components/app/CheckboxField.vue';
import FormDialog from '@/components/app/FormDialog.vue';
import StatusBanner from '@/components/app/StatusBanner.vue';
import TextField from '@/components/app/TextField.vue';
import type { CreateState } from '@/composables/useProjects';
import { choosesTwoPublished } from '@/editor/projectLinks';
import type { ResumeSummary } from '@/editor/resumeApi';
import type { ProjectsCopy } from '@/i18n/projects';
import {
  characterCount,
  PROJECT_LIMIT,
  TEXT_LIMITS,
} from './projectDocument';

const props = defineProps<{
  readonly open: boolean;
  readonly state: CreateState;
  readonly copy: ProjectsCopy;
  /** The account's resumes the new project may be shown on. */
  readonly resumes: readonly ResumeSummary[];
  /** The note the last submit decided to show. */
  readonly privacyNote?: boolean;
}>();
const emit = defineEmits<{
  submit: [name: string, resumeIds: readonly string[]];
  close: [];
}>();

const name = ref('');
const chosen = ref<readonly string[]>([]);
// docs/design/projects.md "Privacy": two published resumes would let a
// reader of both connect them.
const sharesPublic = computed(() =>
  choosesTwoPublished(props.resumes, chosen.value)
  || (props.privacyNote === true && chosen.value.length >= 2));
const valid = computed(() =>
  name.value.trim() !== '' && characterCount(name.value) <= TEXT_LIMITS.name);
const message = computed(() => {
  switch (props.state) {
    case 'limit': return props.copy.limitReached(PROJECT_LIMIT);
    case 'try-again': return props.copy.tryAgain;
    case 'invalid': return props.copy.createInvalid;
    case 'session-lost': return props.copy.sessionLost;
    default: return null;
  }
});

watch(() => props.open, (open) => {
  if (!open) return;
  name.value = '';
  chosen.value = [];
});

function choose(resumeId: string, checked: boolean): void {
  const others = chosen.value.filter((id) => id !== resumeId);
  // Resume list order, so links go out in the order the owner sees.
  chosen.value = props.resumes.map((resume) => resume.id).filter((id) =>
    others.includes(id) || (checked && id === resumeId));
}
</script>

<template>
  <FormDialog
    :open="open"
    :title="copy.createTitle"
    :submit-label="copy.create"
    :cancel-label="copy.cancel"
    :close-label="copy.close"
    :busy="state === 'busy'"
    :submit-disabled="!valid"
    :restore-focus="false"
    submit-action="project-create-submit"
    @cancel="emit('close')"
    @submit="valid && emit('submit', name, chosen)"
  >
    <TextField
      id="project-create-name"
      v-model="name"
      :label="copy.nameLabel"
      :disabled="state === 'busy'"
      autocomplete="off"
      name="name"
      required
    />
    <fieldset
      v-if="resumes.length > 0"
      class="grid gap-3"
      data-testid="project-create-resumes"
    >
      <legend class="mb-2 text-sm font-medium">
        {{ copy.showOnResumes }}
      </legend>
      <div
        v-for="resume in resumes"
        :key="resume.id"
        :data-resume-choice="resume.id"
      >
        <CheckboxField
          :id="`project-create-resume-${resume.id}`"
          :label="resume.title"
          :description="resume.live ? copy.publicMark : copy.draftMark"
          :disabled="state === 'busy'"
          :model-value="chosen.includes(resume.id)"
          @update:model-value="choose(resume.id, $event)"
        />
      </div>
      <p
        v-if="sharesPublic"
        class="rounded-md bg-surface-blue p-3 text-sm"
        data-testid="project-create-privacy"
        role="status"
      >
        {{ copy.sharedPublic }} {{ copy.sharedReader }}
      </p>
    </fieldset>
    <StatusBanner
      v-if="message !== null"
      kind="error"
      testid="project-create-error"
    >
      {{ message }}
    </StatusBanner>
  </FormDialog>
</template>
