<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from 'vue';
import LoadingState from '../app/LoadingState.vue';
import StatusBanner from '../app/StatusBanner.vue';
import { Button } from '../ui/button';
import EvidenceDisconnectDialog from './EvidenceDisconnectDialog.vue';
import {
  createGitHubEvidenceApi,
  type GitHubEvidenceFailure,
  type GitHubEvidenceOutcome,
  type GitHubEvidenceStatus,
} from '../../composables/githubEvidenceApi';
import { sendWithCsrf } from '../../composables/projectsApi';
import { validateAuthorizeUrl } from '../../composables/providerAuthorization';
import { useAuth } from '../../composables/useAuth';
import {
  githubEvidenceSettingsCopy,
} from '../../i18n/github-evidence-settings';
import { privacySettingsCopy } from '../../i18n/privacy-settings';
import { projectsCopy } from '../../i18n/projects';

type Failure = Exclude<GitHubEvidenceFailure, 'session-lost'>;
type Notice = 'connected' | 'failed' | 'different-account';

const auth = useAuth();
const { csrfToken } = auth;
const api = createGitHubEvidenceApi();
const route = useRoute();
const router = useRouter();
const { locale } = useLocale();
const copy = computed(() => githubEvidenceSettingsCopy[locale.value]);
const cancelLabel = computed(() => privacySettingsCopy[locale.value].cancel);

const loading = ref(true);
const loadFailed = ref(false);
const view = ref<GitHubEvidenceStatus | null>(null);
const pending = ref(false);
const failure = ref<Failure | null>(null);
const notice = ref<Notice | null>(null);
const asking = ref(false);
const disconnectButton = ref<{ $el?: HTMLElement } | null>(null);
const connectButton = ref<{ $el?: HTMLElement } | null>(null);
const errorBanner = ref<{ focus?: () => void } | null>(null);
let cancelled = false;
let intent: {
  readonly keepLinks: boolean;
  readonly generation: number;
  readonly key: string;
} | null = null;

const errorMessage = computed(() => {
  if (failure.value === null) return null;
  const errors = copy.value.errors;
  const messages: Record<Failure, string> = {
    'csrf-rejected': errors.csrfRejected,
    'unavailable': errors.unavailable,
    'session-ended': errors.sessionEnded,
    'upstream': errors.upstream,
    'rate-limited': errors.rateLimited,
    'connection-changed': errors.connectionChanged,
    'busy': projectsCopy[locale.value].tryAgain,
    'failed': errors.failed,
  };
  return messages[failure.value];
});

function withCsrf<T>(
  send: (token: string) => Promise<GitHubEvidenceOutcome<T>>,
): Promise<GitHubEvidenceOutcome<T>> {
  const ownerId = auth.user.value?.id;
  if (ownerId === undefined) {
    return Promise.resolve({ kind: 'failed', failure: 'session-lost' });
  }
  return sendWithCsrf(auth, ownerId, send);
}

async function refreshState(): Promise<void> {
  const result = await api.status();
  if (result.kind === 'ok') {
    view.value = result.value;
    loadFailed.value = false;
    return;
  }
  if (result.failure === 'session-lost') {
    await navigateTo('/login');
    return;
  }
  loadFailed.value = true;
}

async function load(): Promise<void> {
  loading.value = true;
  try {
    await refreshState();
  } finally {
    loading.value = false;
  }
}

async function perform<T>(
  send: (token: string) => Promise<GitHubEvidenceOutcome<T>>,
  onDone: (value: T) => Promise<void>,
): Promise<GitHubEvidenceOutcome<T> | null> {
  if (pending.value) return null;
  pending.value = true;
  failure.value = null;
  notice.value = null;
  try {
    const result = await withCsrf(send);
    if (result.kind === 'ok') {
      await onDone(result.value);
      return result;
    }
    if (result.failure === 'session-lost') {
      await navigateTo('/login');
      return result;
    }
    failure.value = result.failure;
    if (
      result.failure === 'session-ended'
      || result.failure === 'connection-changed'
    ) {
      await refreshState();
    }
    return result;
  } finally {
    pending.value = false;
  }
}

function connect(): Promise<unknown> {
  return perform((token) => api.start(token), async (value) => {
    const url = validateAuthorizeUrl('github', value);
    if (url === null) {
      failure.value = 'failed';
      return;
    }
    await navigateTo(url, { external: true });
  });
}

function done(): Promise<unknown> {
  return perform((token) => api.endSession(token), refreshState);
}

function keyFor(keepLinks: boolean, generation: number): string {
  if (intent?.keepLinks !== keepLinks || intent.generation !== generation) {
    intent = { keepLinks, generation, key: crypto.randomUUID() };
  }
  return intent.key;
}

async function sendDisconnect(keepLinks: boolean): Promise<void> {
  const current = view.value;
  if (current === null) return;
  const key = keyFor(keepLinks, current.generation);
  const result = await perform(
    (token) => api.disconnect(keepLinks, current.generation, key, token),
    refreshState,
  );
  if (
    result !== null
    && (result.kind === 'ok'
      || result.failure === 'connection-changed'
      || result.failure === 'session-lost')
  ) {
    intent = null;
  }
}

async function disconnect(): Promise<void> {
  if (view.value?.hasEvidence === true) {
    asking.value = true;
    return;
  }
  await sendDisconnect(true);
  await focusAfterDisconnect();
}

async function focusAfterDisconnect(): Promise<void> {
  await nextTick();
  if (errorMessage.value !== null) {
    errorBanner.value?.focus?.();
    return;
  }
  (disconnectButton.value ?? connectButton.value)?.$el?.focus();
}

async function choose(keepLinks: boolean | null): Promise<void> {
  asking.value = false;
  cancelled = keepLinks === null;
  if (keepLinks === null) return;
  await sendDisconnect(keepLinks);
  await focusAfterDisconnect();
}

async function restoreFocus(): Promise<void> {
  if (!cancelled) return;
  cancelled = false;
  await nextTick();
  disconnectButton.value?.$el?.focus();
}

function readCallbackResult(): void {
  if (!('evidence' in route.query)) return;
  const value = route.query.evidence;
  if (value === 'connected' || value === 'failed') notice.value = value;
  if (value === 'different_account') notice.value = 'different-account';
  const query = Object.fromEntries(
    Object.entries(route.query).filter(([key]) => key !== 'evidence'),
  );
  void router.replace({ path: route.path, query, hash: route.hash });
}

function formatDate(value: string): string {
  return new Intl.DateTimeFormat(locale.value === 'vi' ? 'vi-VN' : 'en-US', {
    timeZone: 'UTC',
    year: 'numeric',
    month: 'long',
    day: 'numeric',
  }).format(new Date(value));
}

function formatClock(value: string): string {
  return new Intl.DateTimeFormat(locale.value === 'vi' ? 'vi-VN' : 'en-US', {
    hour: 'numeric',
    minute: '2-digit',
  }).format(new Date(value));
}

onMounted(() => {
  readCallbackResult();
  void load();
});
</script>

<template>
  <div
    data-testid="github-evidence"
    class="grid gap-4"
  >
    <h2
      id="github-evidence-title"
      class="text-lg font-bold"
    >
      {{ copy.title }}
    </h2>
    <p class="text-muted-foreground text-sm">
      {{ copy.privacy }}
    </p>
    <StatusBanner
      v-if="notice === 'connected'"
      focus-on-mount
      kind="success"
      testid="evidence-connected"
    >
      {{ copy.connectedNotice }}
    </StatusBanner>
    <StatusBanner
      v-else-if="notice === 'failed'"
      focus-on-mount
      kind="error"
      testid="evidence-failed"
    >
      {{ copy.failedNotice }}
    </StatusBanner>
    <StatusBanner
      v-else-if="notice === 'different-account'"
      focus-on-mount
      kind="error"
      testid="evidence-different-account"
    >
      {{ copy.differentAccountNotice }}
    </StatusBanner>
    <StatusBanner
      v-if="errorMessage"
      ref="errorBanner"
      kind="error"
      testid="evidence-error"
    >
      {{ errorMessage }}
    </StatusBanner>

    <LoadingState
      v-if="loading"
      :label="copy.loading"
      testid="evidence-loading"
    />
    <template v-else-if="loadFailed">
      <StatusBanner
        kind="error"
        testid="evidence-load-error"
      >
        {{ copy.loadFailed }}
      </StatusBanner>
      <div>
        <Button
          data-testid="evidence-retry"
          type="button"
          variant="outline"
          @click="load"
        >
          {{ copy.retry }}
        </Button>
      </div>
    </template>
    <template v-else-if="view?.connection">
      <div class="grid gap-1">
        <p class="[overflow-wrap:anywhere]">
          {{ copy.connectedAs }}
          <bdi
            data-testid="evidence-login"
            dir="ltr"
          >{{ view.connection.login }}</bdi>
        </p>
        <p class="text-muted-foreground text-sm">
          {{ copy.connectedOn }}
          <time :datetime="view.connection.connectedAt">{{
            formatDate(view.connection.connectedAt)
          }}</time>
        </p>
        <p
          class="text-muted-foreground text-sm"
          data-testid="evidence-session"
        >
          <template v-if="view.expiresAt">
            {{ copy.sessionOpenUntil }}
            <time :datetime="view.expiresAt">{{
              formatClock(view.expiresAt)
            }}</time>
          </template>
          <template v-else>
            {{ copy.noSession }}
          </template>
        </p>
      </div>
      <div class="flex flex-wrap gap-2">
        <Button
          v-if="view.expiresAt"
          data-testid="evidence-done"
          :disabled="!csrfToken || pending"
          type="button"
          variant="secondary"
          @click="done"
        >
          {{ copy.done }}
        </Button>
        <Button
          v-else
          data-testid="evidence-continue"
          :disabled="!csrfToken || pending"
          type="button"
          variant="outline"
          @click="connect"
        >
          {{ copy.continue }}
        </Button>
        <Button
          ref="disconnectButton"
          data-testid="evidence-disconnect"
          :disabled="!csrfToken || pending"
          type="button"
          variant="destructive"
          @click="disconnect"
        >
          {{ copy.disconnect }}
        </Button>
      </div>
      <EvidenceDisconnectDialog
        :open="asking"
        :title="copy.disconnectAsk"
        :keep-label="copy.disconnectKeep"
        :remove-label="copy.disconnectRemove"
        :cancel-label="cancelLabel"
        @keep="choose(true)"
        @remove="choose(false)"
        @cancel="choose(null)"
        @closed="restoreFocus"
      />
    </template>
    <template v-else-if="view">
      <p class="text-sm">
        {{ copy.notConnected }}
      </p>
      <div>
        <Button
          ref="connectButton"
          data-testid="evidence-connect"
          :disabled="!csrfToken || pending"
          type="button"
          variant="outline"
          @click="connect"
        >
          {{ copy.connect }}
        </Button>
      </div>
    </template>
  </div>
</template>
