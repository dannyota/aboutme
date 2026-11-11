<script setup lang="ts">
import { computed, ref, watch } from 'vue';

import FormDialog from '@/components/app/FormDialog.vue';
import StatusBanner from '@/components/app/StatusBanner.vue';
import TextField from '@/components/app/TextField.vue';
import type { CreateState } from '@/composables/useProjects';
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
}>();
const emit = defineEmits<{ submit: [name: string]; close: [] }>();

const name = ref('');
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
  if (open) name.value = '';
});
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
    @submit="valid && emit('submit', name)"
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
    <StatusBanner
      v-if="message !== null"
      kind="error"
      testid="project-create-error"
    >
      {{ message }}
    </StatusBanner>
  </FormDialog>
</template>
