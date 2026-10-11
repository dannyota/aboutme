<script setup lang="ts">
import { computed, ref, useId, watch } from 'vue';

import FormDialog from '@/components/app/FormDialog.vue';
import SwitchField from '@/components/app/SwitchField.vue';
import TextField from '@/components/app/TextField.vue';
import { Button } from '@/components/ui/button';
import type { ResumeEditorActions } from '../../composables/useResumeEditor';
import {
  canonicalPublicPath,
  type PublishCommand,
} from '../../editor/publishApi';
import { usePublishShowcase } from '../../editor/publishShowcase';
import {
  changedPublicPageFields,
  defaultPublicTitle,
  faviconEmojiIssue,
  publicTitleIssue,
} from '../../editor/publicPageMeta';
import type { ResumeMetadata } from '../../editor/types';
import LocaleToggle from '@/components/app/LocaleToggle.vue';
import { editorShellCopy } from '@/i18n/editor-shell';
import { publishCopy } from '../../i18n/publish';
import PublishAccess from './PublishAccess.vue';
import PublishFeedback from './PublishFeedback.vue';
import PublishPageFields from './PublishPageFields.vue';
import PublishPreview from './PublishPreview.vue';
import PublishReauthentication from './PublishReauthentication.vue';
import PublishResult from './PublishResult.vue';
import PublishShowcase from './PublishShowcase.vue';
import type { ResumeRecord } from '../../stores/resumes';
import { useCapabilities } from '../../composables/useCapabilities';

const props = defineProps<{
  readonly open: boolean;
  readonly actions: ResumeEditorActions;
  readonly record: ResumeRecord;
  readonly photoUrl?: string;
}>();
const { locale } = useLocale();
const copy = computed(() => publishCopy[locale.value]);
const { signInToView: signInToViewCapability } = useCapabilities();

const emit = defineEmits<{
  'close': [];
  'focus-issue': [path: string];
}>();

const live = ref(false);
const downloadEnabled = ref(false);
const seoGeoEnabled = ref(false);
const signInToView = ref(false);
const storedSignInToView = ref(false);
const slug = ref('');
const pageTitle = ref('');
const tabIcon = ref('');
const storedPage = ref<Pick<ResumeMetadata, 'publicTitle' | 'faviconEmoji'>>(
  { publicTitle: null, faviconEmoji: null },
);
const password = ref('');
const actionBusy = ref(false);
const providerLinkActivated = ref(false);
const initialLive = ref(false);
const restoreFocus = ref(true);
const slugInputId = `publish-slug-${useId()}`;
const slugPrefixId = `${slugInputId}-prefix`;

const state = computed(() => props.actions.publish.state.value);
const busy = computed(
  () =>
    actionBusy.value
    || state.value.kind === 'saving'
    || state.value.kind === 'dispatching',
);
const showcase = usePublishShowcase({
  live,
  signInToView,
  busy,
  issues: computed(() =>
    (state.value.kind === 'invalid' ? state.value.issues : [])),
});
const slugValid = computed(
  () =>
    (slug.value === '' && !live.value)
    || canonicalPublicPath(slug.value) !== null,
);
const slugError = computed(() =>
  slugValid.value
    ? undefined
    : copy.value.slugError,
);
const slugDescribedBy = computed(() => [
  slugPrefixId,
  ...(slugError.value === undefined ? [] : [`${slugInputId}-error`]),
].join(' '));
const defaultTitle = computed(() => defaultPublicTitle(
  props.record.current.document.personalDetails.fullName,
));
// Shown when the deployment allows turning the switch on, or the resume
// already has it on, so an owner can always turn it back off (AC-VIEW-001).
const showSignInToView = computed(
  () => signInToViewCapability.value || storedSignInToView.value,
);
const pageFieldsValid = computed(() =>
  publicTitleIssue(pageTitle.value) === null
  && faviconEmojiIssue(tabIcon.value) === null);
const submitDisabled = computed(
  () =>
    busy.value
    || !slugValid.value
    || !pageFieldsValid.value
    || state.value.kind === 'blocked'
    || state.value.kind === 'session-lost',
);
const passwordReauth = computed(() =>
  (state.value.kind === 'reauth-required'
    || state.value.kind === 'reauth-wrong-password'
    || state.value.kind === 'reauth-rate-limited'
    || state.value.kind === 'reauth-unavailable')
  && state.value.method === 'password',
);
const providerReauth = computed(() =>
  (state.value.kind === 'reauth-required'
    || state.value.kind === 'reauth-rate-limited'
    || state.value.kind === 'reauth-unavailable'
    || state.value.kind === 'provider-start-invalid'
    || state.value.kind === 'provider-started-rate-limited')
  && state.value.method === 'provider',
);
const primaryAction = computed(() => {
  if (!live.value && initialLive.value) return copy.value.unpublish;
  if (live.value && initialLive.value) return copy.value.update;
  return copy.value.publish;
});

function syncMetadata(metadata: ResumeRecord['accepted']['metadata']): void {
  initialLive.value = metadata.live;
  live.value = metadata.live;
  downloadEnabled.value = metadata.live && metadata.downloadEnabled;
  seoGeoEnabled.value = metadata.live && metadata.seoGeoEnabled;
  signInToView.value = metadata.signInToView;
  storedSignInToView.value = metadata.signInToView;
  showcase.sync(metadata);
  slug.value = metadata.slug ?? '';
  storedPage.value = {
    publicTitle: metadata.publicTitle,
    faviconEmoji: metadata.faviconEmoji,
  };
  pageTitle.value = metadata.publicTitle ?? '';
  tabIcon.value = metadata.faviconEmoji ?? '';
}

watch(
  () => props.open,
  (open) => {
    if (!open) return;
    syncMetadata(props.record.accepted.metadata);
    password.value = '';
    providerLinkActivated.value = false;
    restoreFocus.value = true;
  },
  { immediate: true },
);

watch(state, (next) => {
  if (next.kind === 'accepted') {
    syncMetadata(next.resume.metadata);
  } else if (
    next.kind === 'stale'
    && props.record.accepted.metadataFreshness === 'complete'
  ) {
    syncMetadata(props.record.accepted.metadata);
  }
});

function setLive(value: boolean): void {
  live.value = value;
  if (!value) {
    downloadEnabled.value = false;
    seoGeoEnabled.value = false;
  }
}

function close(): void {
  if (busy.value) return;
  props.actions.publish.cancel();
  emit('close');
}

async function run(action: () => Promise<unknown>): Promise<void> {
  if (busy.value) return;
  actionBusy.value = true;
  try {
    await action();
  } finally {
    actionBusy.value = false;
  }
}

function command(): PublishCommand {
  return {
    ...(slug.value !== '' ? { slug: slug.value } : {}),
    live: live.value,
    downloadEnabled: live.value && downloadEnabled.value,
    seoGeoEnabled: live.value && seoGeoEnabled.value,
    ...changedPublicPageFields(storedPage.value, {
      publicTitle: pageTitle.value,
      faviconEmoji: tabIcon.value,
    }),
    ...(signInToView.value === storedSignInToView.value
      ? {}
      : { signInToView: signInToView.value }),
    ...showcase.fields(),
  };
}

function submit(): void {
  if (submitDisabled.value) return;
  void run(() => props.actions.publish.submit(command()));
}

function retry(): void {
  if (busy.value) return;
  if (state.value.kind === 'unknown') {
    void run(() => props.actions.publish.retryUncertain());
  } else {
    submit();
  }
}

function submitPassword(): void {
  if (password.value === '' || busy.value) return;
  void run(() => props.actions.publish.reauthPassword(password.value));
}

function startProvider(): void {
  providerLinkActivated.value = false;
  void run(() => props.actions.publish.startProviderReauth());
}

function retryProvider(): void {
  void run(() => props.actions.publish.retryAfterProviderReauth());
}

function focusIssue(path: string): void {
  restoreFocus.value = false;
  close();
  emit('focus-issue', path);
}
</script>

<template>
  <FormDialog
    :open="open"
    class="publish-dialog sm:max-w-[38rem]"
    :title="copy.title"
    :description="copy.description"
    :submit-label="primaryAction"
    :cancel-label="copy.cancel"
    :busy="busy"
    :submit-disabled="submitDisabled"
    :restore-focus="restoreFocus"
    :show-close-button="false"
    submit-action="publish-submit"
    cancel-action="publish-close"
    @submit="submit"
    @cancel="close"
  >
    <template #header-actions>
      <LocaleToggle
        :label="editorShellCopy[locale].localeLabel"
        @pointerdown.prevent
      />
    </template>
    <div class="grid gap-4">
      <div
        class="relative"
        data-testid="publish-slug-field"
      >
        <TextField
          :id="slugInputId"
          v-model="slug"
          :label="copy.slug"
          name="slug"
          autocomplete="off"
          :disabled="busy"
          :error="slugError"
          :control-attrs="{
            'aria-describedby': slugDescribedBy,
            'class': 'pl-28',
            'data-action': 'publish-slug',
            'minlength': '4',
            'maxlength': '30',
            'pattern': '[a-z0-9]+(-[a-z0-9]+)*',
          }"
        />
        <span
          :id="slugPrefixId"
          aria-hidden="true"
          class="pointer-events-none absolute bottom-2 left-3 text-sm
            text-muted-foreground"
          data-testid="publish-slug-prefix"
        >aboutme.vn/</span>
      </div>

      <PublishPageFields
        v-model:emoji="tabIcon"
        v-model:title="pageTitle"
        :default-title="defaultTitle"
        :disabled="busy"
        :copy="copy.page"
        :server-issues="state.kind === 'invalid' ? state.issues : []"
      />

      <PublishPreview
        :document="record.current.document"
        :slug="slug"
        :page-title="pageTitle"
        :lng="record.current.metadata.lng"
        :photo-url="photoUrl"
      />

      <fieldset class="grid gap-3">
        <legend class="mb-1 text-sm font-medium">
          {{ copy.options }}
        </legend>
        <SwitchField
          :model-value="live"
          :label="copy.live"
          name="live"
          data-action="publish-live"
          :disabled="busy"
          :description="copy.liveHelp"
          @update:model-value="setLive"
        />
        <SwitchField
          v-model="downloadEnabled"
          :label="copy.download"
          name="downloadEnabled"
          data-action="publish-download"
          :disabled="busy || !live"
          :description="copy.downloadHelp"
        />
        <SwitchField
          v-model="seoGeoEnabled"
          :label="copy.discovery"
          name="seoGeoEnabled"
          data-action="publish-seo-geo"
          :disabled="busy || !live || signInToView"
          :description="
            signInToView ? copy.discoveryOffForSignIn : copy.discoveryHelp
          "
        />
        <PublishAccess
          v-if="showSignInToView"
          v-model="signInToView"
          :disabled="busy || !live"
          :label="copy.signInToView"
          :description="copy.signInToViewHelp"
        />
        <PublishShowcase
          :busy="busy"
          :copy="copy"
          :issue-code="showcase.issueCode.value"
          :model-value="showcase.on.value"
          :reason="showcase.reason.value"
          :role="showcase.role.value"
          :listed="showcase.listed.value"
          @update:model-value="showcase.setEnabled"
          @update:role="showcase.setRole"
        />
      </fieldset>

      <PublishReauthentication
        v-model:password="password"
        v-model:provider-link-activated="providerLinkActivated"
        :state="state"
        :copy="copy"
        :busy="busy"
        :password-reauth="passwordReauth"
        @submit-password="submitPassword"
        @start-provider="startProvider"
        @retry-provider="retryProvider"
      />
      <PublishFeedback
        :state="state"
        :copy="copy"
        :busy="busy"
        @focus-issue="focusIssue"
      />
      <PublishResult
        :state="state"
        :copy="copy"
        :locale="locale"
      />

      <p
        v-if="busy"
        role="status"
        aria-live="polite"
      >
        {{ copy.publishing }}
      </p>
    </div>

    <template #footer>
      <!-- Primary last: the footer stacks in reverse on phones (DESIGN.md). -->
      <Button
        type="button"
        variant="outline"
        data-action="publish-close"
        :disabled="busy"
        @click="close"
      >
        {{ copy.cancel }}
      </Button>
      <Button
        v-if="
          state.kind === 'unknown'
            || state.kind === 'rate-limited'
            || state.kind === 'public-state-busy'
            || state.kind === 'slug-taken'
        "
        type="button"
        variant="secondary"
        data-action="publish-retry"
        :disabled="busy"
        @click="retry"
      >
        {{ copy.retry }}
      </Button>
      <Button
        v-if="
          (state.kind === 'reauth-unavailable'
            && state.method === 'provider')
            || state.kind === 'provider-start-invalid'
            || state.kind === 'provider-started-rate-limited'
        "
        type="button"
        variant="secondary"
        data-action="publish-provider-start"
        :disabled="busy"
        @click="startProvider"
      >
        {{ copy.providerRetryAction }}
      </Button>
      <Button
        v-if="
          !passwordReauth
            && !providerReauth
            && state.kind !== 'provider-started'
            && state.kind !== 'unknown'
        "
        type="submit"
        data-action="publish-submit"
        :disabled="submitDisabled"
        variant="seal"
      >
        {{ primaryAction }}
      </Button>
    </template>
  </FormDialog>
</template>
