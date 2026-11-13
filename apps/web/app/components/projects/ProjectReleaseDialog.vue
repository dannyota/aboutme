<script setup lang="ts">
import { computed, ref, watch } from 'vue';

import FormDialog from '@/components/app/FormDialog.vue';
import StatusBanner from '@/components/app/StatusBanner.vue';
import TextField from '@/components/app/TextField.vue';
import PublishReauthentication
  from '@/components/editor/PublishReauthentication.vue';
import { Button } from '@/components/ui/button';
import type { ProjectsCopy } from '@/i18n/projects';
import type { ReleaseAction, ReleaseKind } from '@/composables/projectsApi';
import { isProjectSlug, projectName, type Project } from './projectDocument';

const props = defineProps<{
  readonly open: boolean;
  readonly kind: ReleaseKind;
  readonly project: Project | null;
  readonly action: ReleaseAction;
  readonly copy: ProjectsCopy;
  readonly linkedCount?: number;
}>();
const emit = defineEmits<{ close: [] }>();

const slug = ref('');
const password = ref('');
const providerLinkActivated = ref(false);

const state = computed(() => props.action.state.value);
const busy = computed(() => state.value.kind === 'busy');
const method = computed(() =>
  'method' in state.value ? state.value.method : null);
const passwordReauth = computed(() => method.value === 'password');
const providerStep = computed(() =>
  method.value === 'provider' || state.value.kind === 'provider-started');
const secondFactorStep = computed(() =>
  state.value.kind === 'reauth-second-factor');
const reauthStep = computed(() =>
  passwordReauth.value || providerStep.value || secondFactorStep.value);
const name = computed(() =>
  props.project === null
    ? ''
    : projectName(props.project).trim() || props.copy.untitled);
const slugError = computed(() => {
  if (props.kind !== 'address') return undefined;
  if (!isProjectSlug(slug.value)) return props.copy.addressInvalid;
  if (state.value.kind === 'slug-taken') return props.copy.slugTaken;
  if (state.value.kind === 'slug-held') return props.copy.slugHeld;
  return undefined;
});
const linkedCount = computed(() =>
  props.linkedCount ?? props.project?.linkedResumeIds?.length ?? 0);
const canSubmit = computed(() =>
  props.project !== null
  && (props.kind === 'delete'
    || (isProjectSlug(slug.value) && slug.value !== props.project.slug)));
const message = computed(() => {
  switch (state.value.kind) {
    case 'stale': return props.copy.changedElsewhere;
    case 'not-found': return props.copy.notFound;
    case 'invalid': return props.copy.saveInvalid;
    case 'try-again': return props.copy.tryAgain;
    case 'session-lost': return props.copy.sessionLost;
    case 'no-reauth-method': return props.copy.noReauthMethod;
    default: return null;
  }
});
const retryProviderStart = computed(() =>
  (state.value.kind === 'reauth-unavailable' && method.value === 'provider')
  || state.value.kind === 'provider-start-invalid'
  || state.value.kind === 'provider-started-rate-limited');

watch(() => [props.open, props.project?.id] as const, ([open]) => {
  if (!open) return;
  props.action.reset();
  slug.value = props.project?.slug ?? '';
  password.value = '';
  providerLinkActivated.value = false;
}, { immediate: true });

watch(slug, () => {
  if (state.value.kind === 'slug-taken' || state.value.kind === 'slug-held') {
    props.action.reset();
  }
});

function submit(): void {
  if (props.project === null || !canSubmit.value) return;
  void props.action.start(props.kind, props.project, slug.value);
}

function submitPassword(): void {
  void props.action.reauthPassword(password.value).then(() => {
    password.value = '';
  });
}

function close(): void {
  props.action.reset();
  emit('close');
}
</script>

<template>
  <FormDialog
    :open="open"
    :title="kind === 'address' ? copy.addressTitle : copy.deleteTitle"
    :description="kind === 'delete' ? copy.deleteDescription(name) : undefined"
    :submit-label="kind === 'address' ? copy.changeAddress : copy.deleteProject"
    :cancel-label="copy.cancel"
    :close-label="copy.close"
    :busy="busy"
    :restore-focus="false"
    @cancel="close"
    @submit="submit"
  >
    <template v-if="kind === 'address'">
      <TextField
        id="project-new-address"
        v-model="slug"
        :label="copy.newAddress"
        :hint="copy.addressHint"
        :error="slugError"
        :disabled="busy || reauthStep"
        autocomplete="off"
        name="slug"
        :control-attrs="{ 'data-action': 'project-new-address' }"
      />
      <p class="text-sm text-muted-foreground">
        {{ copy.addressNote }}
      </p>
    </template>
    <p
      v-if="kind === 'delete' && linkedCount > 0"
      class="text-sm"
      data-testid="project-delete-linked"
    >
      {{ copy.deleteLinked(linkedCount) }}
    </p>
    <p
      v-if="reauthStep"
      class="text-sm"
    >
      {{ copy.reauthIntro }}
    </p>
    <PublishReauthentication
      v-model:password="password"
      v-model:provider-link-activated="providerLinkActivated"
      :state="state"
      :copy="copy.reauth"
      :busy="busy"
      :password-reauth="passwordReauth"
      action-prefix="project"
      @submit-password="submitPassword"
      @start-provider="action.startProviderReauth()"
      @retry-provider="action.retryAfterProviderReauth()"
      @continue-second-factor="action.continueAfterSecondFactor()"
    />
    <StatusBanner
      v-if="message !== null"
      kind="error"
      testid="project-release-error"
    >
      {{ message }}
    </StatusBanner>
    <p
      v-if="busy"
      role="status"
      class="text-sm text-muted-foreground"
    >
      {{ copy.working }}
    </p>

    <template #footer>
      <Button
        type="button"
        variant="outline"
        data-action="project-release-cancel"
        :disabled="busy"
        @click="close"
      >
        {{ copy.cancel }}
      </Button>
      <Button
        v-if="retryProviderStart"
        type="button"
        variant="secondary"
        data-action="project-provider-start"
        :disabled="busy"
        @click="action.startProviderReauth()"
      >
        {{ copy.reauth.providerRetryAction }}
      </Button>
      <Button
        v-if="!reauthStep"
        type="submit"
        :data-action="`project-${kind}-submit`"
        :disabled="busy || !canSubmit"
        :variant="kind === 'delete' ? 'destructive' : 'default'"
      >
        {{ kind === 'address' ? copy.changeAddress : copy.deleteProject }}
      </Button>
    </template>
  </FormDialog>
</template>
