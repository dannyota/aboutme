<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue';

import type { ResumeEditorActions } from '../../composables/useResumeEditor';
import {
  canonicalPublicPath,
  type PublishCommand,
} from '../../editor/publishApi';
import type { PublishControllerState } from '../../editor/publishController';
import type { ResumeRecord } from '../../stores/resumes';

const props = defineProps<{
  readonly open: boolean;
  readonly actions: ResumeEditorActions;
  readonly record: ResumeRecord;
}>();

const emit = defineEmits<{
  'close': [];
  'focus-issue': [path: string];
}>();

const live = ref(false);
const downloadEnabled = ref(false);
const seoGeoEnabled = ref(false);
const slug = ref('');
const password = ref('');
const actionBusy = ref(false);
const providerLinkActivated = ref(false);
const initialLive = ref(false);
const slugInput = ref<HTMLInputElement | null>(null);
const returnFocus = ref<HTMLElement | null>(null);

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
const submitDisabled = computed(
  () =>
    busy.value
    || !slugValid.value
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
  if (!live.value && initialLive.value) return 'Unpublish';
  if (live.value && initialLive.value) return 'Update publication';
  return 'Publish';
});
const publicHref = computed(() => {
  const result = state.value;
  return result.kind === 'accepted' && result.resume.metadata.live
    ? canonicalPublicPath(result.resume.metadata.slug)
    : null;
});

function syncMetadata(metadata: ResumeRecord['accepted']['metadata']): void {
  initialLive.value = metadata.live;
  live.value = metadata.live;
  downloadEnabled.value = metadata.live && metadata.downloadEnabled;
  seoGeoEnabled.value = metadata.live && metadata.seoGeoEnabled;
  slug.value = metadata.slug ?? '';
}

watch(
  () => props.open,
  (open) => {
    if (open) {
      const metadata = props.record.accepted.metadata;
      syncMetadata(metadata);
      password.value = '';
      providerLinkActivated.value = false;
      returnFocus.value
        = document.activeElement instanceof HTMLElement
          ? document.activeElement
          : null;
      void nextTick(() => slugInput.value?.focus());
    } else if (returnFocus.value !== null) {
      const target = returnFocus.value;
      returnFocus.value = null;
      void nextTick(() => target.focus());
    }
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

function onKeydown(event: KeyboardEvent): void {
  if (event.key === 'Escape') {
    event.preventDefault();
    close();
    return;
  }
  if (event.key !== 'Tab') return;
  const dialog = event.currentTarget instanceof HTMLElement
    ? event.currentTarget.querySelector<HTMLElement>('[role="dialog"]')
    : null;
  if (dialog === null) return;
  const focusable = [...dialog.querySelectorAll<HTMLElement>(
    [
      'a[href]',
      'button:not(:disabled)',
      'input:not(:disabled)',
      'select:not(:disabled)',
      'textarea:not(:disabled)',
    ].join(', '),
  )];
  if (focusable.length === 0) return;
  const first = focusable[0];
  const last = focusable[focusable.length - 1];
  if (first === undefined || last === undefined) return;
  if (event.shiftKey && document.activeElement === first) {
    event.preventDefault();
    last.focus();
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault();
    first.focus();
  }
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
  returnFocus.value = null;
  close();
  emit('focus-issue', path);
}

function issueMessage(code: string): string {
  switch (code) {
    case 'required_for_live':
      return 'A required field is missing for publication.';
    case 'requires_live':
      return 'This option requires Public resume.';
    case 'invalid_format':
      return 'The slug format is invalid.';
    case 'reserved':
      return 'That slug is reserved.';
    case 'required':
      return 'A required field is missing.';
    case 'visible_entry_required':
      return 'Add a visible resume entry before publishing.';
    default:
      return 'The resume cannot be published.';
  }
}

function blockedMessage(
  reason: Extract<PublishControllerState, { kind: 'blocked' }>['reason'],
): string {
  switch (reason) {
    case 'not-loaded':
      return 'The resume is still loading. Publishing cannot start yet.';
    case 'saving':
      return 'Save the latest resume changes before publishing.';
    case 'conflict':
      return 'Resolve the resume conflict before publishing.';
    case 'session-lost':
      return 'Your session ended. Sign in again before publishing.';
    case 'issue':
      return 'Resolve the current resume issues before publishing.';
    case 'partial-template':
      return 'Finish recovering the template changes before publishing.';
    case 'opaque-photo':
      return 'Resolve the photo change before publishing.';
    case 'read-required':
      return 'Refresh the complete resume before publishing.';
  }
}

function failureMessage(code: string): string {
  switch (code) {
    case 'provider_disabled':
    case 'provider_unavailable':
      return [
        'No supported reauthentication method is available.',
        'Publishing is unavailable.',
      ].join(' ');
    case 'csrf_rejected':
      return [
        'We could not verify this action.',
        'Refresh your session and try again.',
      ].join(' ');
    case 'save_failed':
      return 'We could not save the resume before publishing. Try again.';
    default:
      return 'Publishing failed. Try again.';
  }
}
</script>

<template>
  <div
    v-if="open"
    class="publish-dialog-backdrop"
    @keydown="onKeydown"
  >
    <section
      class="publish-dialog"
      role="dialog"
      aria-modal="true"
      aria-labelledby="publish-dialog-title"
      aria-describedby="publish-dialog-description"
    >
      <h2 id="publish-dialog-title">
        Publish resume
      </h2>
      <p id="publish-dialog-description">
        Choose how this resume is shared publicly.
      </p>

      <form @submit.prevent="submit">
        <label>
          Slug
          <input
            ref="slugInput"
            v-model="slug"
            data-action="publish-slug"
            name="slug"
            minlength="4"
            maxlength="30"
            pattern="[a-z0-9]+(-[a-z0-9]+)*"
            autocomplete="off"
            :aria-invalid="!slugValid"
            :disabled="busy"
          >
        </label>

        <fieldset :disabled="busy">
          <legend>Publish options</legend>
          <label>
            <input
              type="checkbox"
              data-action="publish-live"
              name="live"
              :checked="live"
              @change="setLive(($event.target as HTMLInputElement).checked)"
            >
            Public resume
          </label>
          <label>
            <input
              v-model="downloadEnabled"
              type="checkbox"
              data-action="publish-download"
              name="downloadEnabled"
              :disabled="!live"
            >
            PDF download
          </label>
          <label>
            <input
              v-model="seoGeoEnabled"
              type="checkbox"
              data-action="publish-seo-geo"
              name="seoGeoEnabled"
              :disabled="!live"
            >
            SEO and GEO
          </label>
        </fieldset>

        <p>
          Public resumes may be delivered through a global content-delivery
          network.
        </p>
        <p>
          SEO and GEO allow search crawlers and AI answer engines to discover
          and reuse public resume content.
        </p>

        <p
          v-if="!slugValid"
          role="alert"
          aria-live="polite"
        >
          Use 4–30 lowercase ASCII letters or numbers, separated by single
          hyphens.
        </p>

        <div
          v-if="passwordReauth"
          class="publish-dialog__reauth"
        >
          <label>
            Current password
            <input
              v-model="password"
              type="password"
              autocomplete="current-password"
              :disabled="busy"
            >
          </label>
          <button
            type="button"
            data-action="publish-password-reauth"
            :disabled="busy || password === ''"
            @click="submitPassword"
          >
            Reauthenticate and publish
          </button>
        </div>

        <div
          v-else-if="
            (state.kind === 'reauth-required'
              || state.kind === 'reauth-rate-limited')
              && state.method === 'provider'
          "
          class="publish-dialog__reauth"
        >
          <p>Continue with your linked provider to reauthenticate.</p>
          <button
            type="button"
            data-action="publish-provider-start"
            :disabled="busy"
            @click="startProvider"
          >
            Start provider reauthentication
          </button>
        </div>

        <div
          v-else-if="state.kind === 'provider-started'"
          class="publish-dialog__reauth"
        >
          <a
            :href="state.authorizeUrl"
            data-action="publish-provider-link"
            target="_blank"
            rel="noopener noreferrer"
            @click="providerLinkActivated = true"
          >
            Continue reauthentication in a new tab
          </a>
          <p v-if="providerLinkActivated">
            Finish reauthentication in the new tab, return to the editor, and
            choose Retry publish.
          </p>
          <button
            v-if="providerLinkActivated"
            type="button"
            data-action="publish-provider-retry"
            :disabled="busy"
            @click="retryProvider"
          >
            Retry publish
          </button>
        </div>

        <p
          v-if="state.kind === 'reauth-wrong-password'"
          role="alert"
        >
          That password was not accepted. Try again.
        </p>
        <p
          v-if="
            state.kind === 'reauth-rate-limited'
              || state.kind === 'provider-started-rate-limited'
          "
          role="alert"
        >
          Reauthentication is temporarily rate limited. Try again shortly.
        </p>
        <p
          v-if="
            state.kind === 'reauth-unavailable'
              || state.kind === 'provider-start-invalid'
          "
          role="alert"
        >
          Reauthentication is unavailable. Try again later.
        </p>

        <p
          v-if="state.kind === 'blocked'"
          role="alert"
        >
          {{ blockedMessage(state.reason) }}
        </p>
        <div
          v-if="state.kind === 'invalid'"
          role="alert"
        >
          <p>The resume cannot be published yet.</p>
          <button
            v-for="issue in state.issues"
            :key="`${issue.path}-${issue.code}`"
            type="button"
            data-action="focus-publish-issue"
            :disabled="busy"
            @click="focusIssue(issue.path)"
          >
            {{ issueMessage(issue.code) }}
          </button>
        </div>
        <p
          v-if="state.kind === 'slug-taken'"
          role="alert"
        >
          That public slug is already in use. Choose another slug.
        </p>
        <p
          v-if="state.kind === 'stale'"
          role="alert"
        >
          The resume changed elsewhere. Review the latest version before
          publishing again.
        </p>
        <p
          v-if="
            state.kind === 'rate-limited' || state.kind === 'public-state-busy'
          "
          role="alert"
        >
          Publishing is temporarily unavailable. Try again shortly.
        </p>
        <p
          v-if="state.kind === 'unknown'"
          role="alert"
        >
          We could not confirm publication. Retry publish to check safely.
        </p>
        <p
          v-if="state.kind === 'session-lost'"
          role="alert"
        >
          Your session ended. Sign in again before publishing.
        </p>
        <p
          v-if="state.kind === 'failed'"
          role="alert"
        >
          {{ failureMessage(state.code) }}
        </p>

        <p
          v-if="state.kind === 'accepted'"
          class="publish-dialog__success"
          role="status"
          aria-live="polite"
        >
          <template v-if="state.resume.metadata.live">
            Published successfully.
          </template>
          <template v-else>
            Resume is private.
          </template>
          <a
            v-if="publicHref !== null"
            :href="publicHref"
          >
            View public resume
          </a>
        </p>

        <div class="publish-dialog__actions">
          <button
            v-if="
              !passwordReauth
                && !providerReauth
                && state.kind !== 'provider-started'
                && state.kind !== 'unknown'
            "
            type="submit"
            data-action="publish-submit"
            :disabled="submitDisabled"
          >
            {{ primaryAction }}
          </button>
          <button
            v-if="
              state.kind === 'unknown'
                || state.kind === 'rate-limited'
                || state.kind === 'public-state-busy'
                || state.kind === 'slug-taken'
            "
            type="button"
            :disabled="busy"
            @click="retry"
          >
            Retry publish
          </button>
          <button
            v-if="
              (state.kind === 'reauth-unavailable'
                && state.method === 'provider')
                || state.kind === 'provider-start-invalid'
                || state.kind === 'provider-started-rate-limited'
            "
            type="button"
            data-action="publish-provider-start"
            :disabled="busy"
            @click="startProvider"
          >
            Try provider reauthentication again
          </button>
          <button
            type="button"
            data-action="publish-close"
            :disabled="busy"
            @click="close"
          >
            Close
          </button>
        </div>
        <p
          v-if="busy"
          role="status"
          aria-live="polite"
        >
          Publishing…
        </p>
      </form>
    </section>
  </div>
</template>
