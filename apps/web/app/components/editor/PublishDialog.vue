<script setup lang="ts">
import { computed, onBeforeUnmount, ref, useId, watch } from 'vue';

import AppSeal from '@/components/app/AppSeal.vue';
import FormDialog from '@/components/app/FormDialog.vue';
import SwitchField from '@/components/app/SwitchField.vue';
import TextField from '@/components/app/TextField.vue';
import { Button } from '@/components/ui/button';
import type { ResumeEditorActions } from '../../composables/useResumeEditor';
import {
  canonicalPublicPath,
  type PublishCommand,
} from '../../editor/publishApi';
import type { PublishControllerState } from '../../editor/publishController';
import {
  changedPublicPageFields,
  defaultPublicTitle,
  faviconEmojiIssue,
  publicPageIssueField,
  publicTitleIssue,
} from '../../editor/publicPageMeta';
import type { ResumeMetadata } from '../../editor/types';
import LocaleToggle from '@/components/app/LocaleToggle.vue';
import { editorShellCopy } from '@/i18n/editor-shell';
import { publishCopy } from '../../i18n/publish';
import { workspaceCopy } from '../../i18n/workspace';
import PublishPageFields from './PublishPageFields.vue';
import type { ResumeRecord } from '../../stores/resumes';

const props = defineProps<{
  readonly open: boolean;
  readonly actions: ResumeEditorActions;
  readonly record: ResumeRecord;
}>();
const { locale } = useLocale();
const copy = computed(() => publishCopy[locale.value]);

const emit = defineEmits<{
  'close': [];
  'focus-issue': [path: string];
}>();

const live = ref(false);
const downloadEnabled = ref(false);
const seoGeoEnabled = ref(false);
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
const copyState = ref<'idle' | 'copied' | 'failed'>('idle');
const slugInputId = `publish-slug-${useId()}`;
const slugPrefixId = `${slugInputId}-prefix`;
let copyResetTimer: ReturnType<typeof setTimeout> | undefined;

const state = computed(() => props.actions.publish.state.value);
const busy = computed(
  () =>
    actionBusy.value
    || state.value.kind === 'saving'
    || state.value.kind === 'dispatching',
);
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
const pageFieldsValid = computed(() =>
  publicTitleIssue(pageTitle.value) === null
  && faviconEmojiIssue(tabIcon.value) === null);
// Issues on the tab fields show at the fields; the rest keep their list.
const listedIssues = computed(() =>
  state.value.kind === 'invalid'
    ? state.value.issues.filter((issue) =>
        publicPageIssueField(issue.path) === null)
    : []);
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
const publicHref = computed(() => {
  const result = state.value;
  return result.kind === 'accepted' && result.resume.metadata.live
    ? canonicalPublicPath(result.resume.metadata.slug)
    : null;
});
const publicSealLabel = computed(() => {
  const link = publicHref.value;
  return link === null ? '' : workspaceCopy[locale.value].publicAt(link);
});

function syncMetadata(metadata: ResumeRecord['accepted']['metadata']): void {
  initialLive.value = metadata.live;
  live.value = metadata.live;
  downloadEnabled.value = metadata.live && metadata.downloadEnabled;
  seoGeoEnabled.value = metadata.live && metadata.seoGeoEnabled;
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

watch(publicHref, () => resetCopyState());

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

async function copyLink(): Promise<void> {
  const path = publicHref.value;
  if (path === null) return;
  resetCopyState();
  try {
    await navigator.clipboard.writeText(`${location.origin}${path}`);
    copyState.value = 'copied';
    copyResetTimer = setTimeout(() => {
      copyState.value = 'idle';
      copyResetTimer = undefined;
    }, 2_000);
  } catch {
    copyState.value = 'failed';
  }
}

function resetCopyState(): void {
  if (copyResetTimer !== undefined) {
    clearTimeout(copyResetTimer);
    copyResetTimer = undefined;
  }
  copyState.value = 'idle';
}

function issueMessage(code: string): string {
  switch (code) {
    case 'required_for_live':
    case 'requires_live':
    case 'invalid_format':
    case 'reserved':
    case 'required':
    case 'visible_entry_required':
      return copy.value.issue[code];
    default:
      return copy.value.invalid;
  }
}

function blockedMessage(
  reason: Extract<PublishControllerState, { kind: 'blocked' }>['reason'],
): string {
  return copy.value.blocked[reason];
}

function failureMessage(code: string): string {
  switch (code) {
    case 'provider_disabled':
    case 'provider_unavailable':
    case 'csrf_rejected':
    case 'save_failed':
      return copy.value.failed[code];
    default:
      return copy.value.failed.generic;
  }
}

onBeforeUnmount(resetCopyState);
</script>

<template>
  <FormDialog
    :open="open"
    class="publish-dialog max-h-[calc(100dvh-2rem)] overflow-y-auto
      sm:max-w-[38rem]"
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
          :disabled="busy || !live"
          :description="copy.discoveryHelp"
        />
      </fieldset>

      <div
        v-if="passwordReauth"
        class="grid gap-3"
      >
        <TextField
          v-model="password"
          :label="copy.currentPassword"
          type="password"
          autocomplete="current-password"
          :disabled="busy"
          :control-attrs="{ 'data-action': 'publish-password' }"
        />
        <Button
          type="button"
          data-action="publish-password-reauth"
          :disabled="busy || password === ''"
          @click="submitPassword"
        >
          {{ copy.passwordAction }}
        </Button>
      </div>

      <div
        v-else-if="
          (state.kind === 'reauth-required'
            || state.kind === 'reauth-rate-limited')
            && state.method === 'provider'
        "
        class="grid gap-3"
      >
        <p>{{ copy.providerContinue }}</p>
        <Button
          type="button"
          data-action="publish-provider-start"
          :disabled="busy"
          @click="startProvider"
        >
          {{ copy.providerStart }}
        </Button>
      </div>

      <div
        v-else-if="state.kind === 'provider-started'"
        class="grid gap-3"
      >
        <a
          :href="state.authorizeUrl"
          class="text-link underline underline-offset-4"
          data-action="publish-provider-link"
          target="_blank"
          rel="noopener noreferrer"
          @click="providerLinkActivated = true"
        >
          {{ copy.providerLink }}
        </a>
        <p v-if="providerLinkActivated">
          {{ copy.providerReturn }}
        </p>
        <Button
          v-if="providerLinkActivated"
          type="button"
          data-action="publish-provider-retry"
          :disabled="busy"
          @click="retryProvider"
        >
          {{ copy.providerRetry }}
        </Button>
      </div>

      <p
        v-if="state.kind === 'reauth-wrong-password'"
        role="alert"
      >
        {{ copy.wrongPassword }}
      </p>
      <p
        v-if="
          state.kind === 'reauth-rate-limited'
            || state.kind === 'provider-started-rate-limited'
        "
        role="alert"
      >
        {{ copy.reauthRateLimited }}
      </p>
      <p
        v-if="
          state.kind === 'reauth-unavailable'
            || state.kind === 'provider-start-invalid'
        "
        role="alert"
      >
        {{ copy.reauthUnavailable }}
      </p>

      <p
        v-if="state.kind === 'blocked'"
        role="alert"
      >
        {{ blockedMessage(state.reason) }}
      </p>
      <div
        v-if="listedIssues.length > 0"
        role="alert"
        class="grid gap-2"
      >
        <p>{{ copy.invalid }}</p>
        <Button
          v-for="issue in listedIssues"
          :key="`${issue.path}-${issue.code}`"
          type="button"
          variant="outline"
          data-action="focus-publish-issue"
          :disabled="busy"
          @click="focusIssue(issue.path)"
        >
          {{ issueMessage(issue.code) }}
        </Button>
      </div>
      <p
        v-if="state.kind === 'slug-taken'"
        role="alert"
      >
        {{ copy.slugTaken }}
      </p>
      <p
        v-if="state.kind === 'stale'"
        role="alert"
      >
        {{ copy.stale }}
      </p>
      <p
        v-if="
          state.kind === 'rate-limited' || state.kind === 'public-state-busy'
        "
        role="alert"
      >
        {{ copy.rateLimited }}
      </p>
      <p
        v-if="state.kind === 'unknown'"
        role="alert"
      >
        {{ copy.unknown }}
      </p>
      <p
        v-if="state.kind === 'session-lost'"
        role="alert"
      >
        {{ copy.sessionLost }}
      </p>
      <p
        v-if="state.kind === 'failed'"
        role="alert"
      >
        {{ failureMessage(state.code) }}
      </p>

      <div
        v-if="state.kind === 'accepted'"
        class="publish-dialog__success grid justify-items-start gap-3"
      >
        <p
          aria-live="polite"
          role="status"
        >
          <template v-if="state.resume.metadata.live">
            {{ copy.published }}
          </template>
          <template v-else>
            {{ copy.private }}
          </template>
        </p>
        <template v-if="publicHref !== null">
          <AppSeal
            :link="publicHref"
            :label="publicSealLabel"
            size="stamp"
          />
          <a
            :href="publicHref"
            class="text-link underline underline-offset-4"
            data-action="view-public-resume"
          >aboutme.vn{{ publicHref }}</a>
          <Button
            data-action="copy-link"
            type="button"
            variant="secondary"
            @click="copyLink"
          >
            {{ copyState === 'copied' ? copy.copied : copy.copyLink }}
          </Button>
          <p
            v-if="copyState === 'failed'"
            data-testid="copy-link-error"
            role="alert"
          >
            {{ copy.copyFailed }}
          </p>
        </template>
      </div>

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
