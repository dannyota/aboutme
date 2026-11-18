<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import LoadingState from '../app/LoadingState.vue';
import StatusBanner from '../app/StatusBanner.vue';
import { Button } from '../ui/button';
import { createApiClient } from '../../api/client';
import { sendWithCsrf } from '../../composables/projectsApi';
import { validateAuthorizeUrl } from '../../composables/providerAuthorization';
import { useAuth } from '../../composables/useAuth';
import {
  githubEvidenceSettingsCopy,
} from '../../i18n/github-evidence-settings';

interface EvidenceView {
  readonly generation: number;
  readonly connection: {
    readonly login: string;
    readonly connectedAt: string;
  } | null;
  readonly expiresAt: string | null;
}

type Failure
  = | 'session-lost'
    | 'csrf-rejected'
    | 'unavailable'
    | 'session-ended'
    | 'upstream'
    | 'rate-limited'
    | 'connection-changed'
    | 'failed';

type Outcome<T>
  = | { readonly kind: 'ok'; readonly value: T }
    | { readonly kind: 'failed'; readonly failure: Failure };

interface RawResult {
  readonly data?: unknown;
  readonly error?: unknown;
  readonly response: Response;
}

const auth = useAuth();
const { csrfToken } = auth;
const api = createApiClient();
const route = useRoute();
const router = useRouter();
const { locale } = useLocale();
const copy = computed(() => githubEvidenceSettingsCopy[locale.value]);

const loading = ref(true);
const loadFailed = ref(false);
const view = ref<EvidenceView | null>(null);
const pending = ref(false);
const failure = ref<Exclude<Failure, 'session-lost'> | null>(null);
type Notice = 'connected' | 'failed' | 'different-account';

const notice = ref<Notice | null>(null);

const errorMessage = computed(() => {
  if (failure.value === null) return null;
  const errors = copy.value.errors;
  const messages: Record<Exclude<Failure, 'session-lost'>, string> = {
    'csrf-rejected': errors.csrfRejected,
    'unavailable': errors.unavailable,
    'session-ended': errors.sessionEnded,
    'upstream': errors.upstream,
    'rate-limited': errors.rateLimited,
    'connection-changed': errors.failed,
    'failed': errors.failed,
  };
  return messages[failure.value];
});

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function isIsoDate(value: unknown): value is string {
  return typeof value === 'string'
    && value !== ''
    && Number.isFinite(Date.parse(value));
}

function connectionFrom(
  value: unknown,
): EvidenceView['connection'] | undefined {
  if (value === null) return null;
  if (!isRecord(value)) return undefined;
  const { githubUserId, login, connectedAt } = value;
  return typeof githubUserId === 'number'
    && typeof login === 'string'
    && login !== ''
    && isIsoDate(connectedAt)
    ? { login, connectedAt }
    : undefined;
}

function expiryFrom(value: unknown): string | null | undefined {
  if (value === null) return null;
  if (!isRecord(value)) return undefined;
  const { expiresAt } = value;
  return isIsoDate(expiresAt) ? expiresAt : undefined;
}

function viewFrom(data: unknown): EvidenceView | undefined {
  if (!isRecord(data) || !isRecord(data.data)) return undefined;
  const { generation } = data.data;
  if (
    typeof generation !== 'number'
    || !Number.isSafeInteger(generation)
    || generation < 0
  ) {
    return undefined;
  }
  const connection = connectionFrom(data.data.connection);
  const expiresAt = expiryFrom(data.data.session);
  return connection === undefined || expiresAt === undefined
    ? undefined
    : { generation, connection, expiresAt };
}

function authorizeUrlFrom(data: unknown): unknown {
  return isRecord(data) && isRecord(data.data) ? data.data.authorizeUrl : null;
}

function failureOf(status: number, error: unknown): Failure {
  const code = isRecord(error) && isRecord(error.error)
    ? error.error.code
    : undefined;
  if (status === 401) return 'session-lost';
  if (status === 403 && code === 'csrf_rejected') return 'csrf-rejected';
  if (code === 'evidence_unavailable') return 'unavailable';
  if (code === 'evidence_session_required') return 'session-ended';
  if (code === 'rate_limited') return 'rate-limited';
  if (code === 'evidence_upstream') return 'upstream';
  if (code === 'evidence_connection_changed') return 'connection-changed';
  return 'failed';
}

async function settle<T>(
  run: () => Promise<RawResult>,
  parse: (data: unknown) => T | undefined,
): Promise<Outcome<T>> {
  let result: RawResult;
  try {
    result = await run();
  } catch {
    return { kind: 'failed', failure: 'failed' };
  }
  if (!result.response.ok) {
    return {
      kind: 'failed',
      failure: failureOf(result.response.status, result.error),
    };
  }
  const value = parse(result.data);
  return value === undefined
    ? { kind: 'failed', failure: 'failed' }
    : { kind: 'ok', value };
}

function withCsrf<T>(
  send: (token: string) => Promise<Outcome<T>>,
): Promise<Outcome<T>> {
  const ownerId = auth.user.value?.id;
  if (ownerId === undefined) {
    return Promise.resolve({ kind: 'failed', failure: 'session-lost' });
  }
  return sendWithCsrf(auth, ownerId, send);
}

function read(): Promise<Outcome<EvidenceView>> {
  return settle(
    () => api.GET('/github-evidence', {
      cache: 'no-store',
    }) as Promise<RawResult>,
    viewFrom,
  );
}

function start(token: string): Promise<Outcome<unknown>> {
  return settle(
    () => api.POST('/github-evidence/start', {
      headers: { 'X-CSRF-Token': token },
      cache: 'no-store',
    }) as Promise<RawResult>,
    authorizeUrlFrom,
  );
}

function endSession(token: string): Promise<Outcome<null>> {
  return settle(
    () => api.POST('/github-evidence/session/end', {
      headers: { 'X-CSRF-Token': token },
      cache: 'no-store',
    }) as Promise<RawResult>,
    () => null,
  );
}

function disconnectAccount(
  generation: number,
  key: string,
): (token: string) => Promise<Outcome<null>> {
  return (token) => settle(
    () => api.DELETE('/github-evidence', {
      params: {
        query: { keepLinks: 'true', generation },
        header: { 'Idempotency-Key': key },
      },
      headers: { 'X-CSRF-Token': token },
      cache: 'no-store',
    }) as Promise<RawResult>,
    () => null,
  );
}

async function refreshState(): Promise<void> {
  const result = await read();
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
  send: (token: string) => Promise<Outcome<T>>,
  onDone: (value: T) => Promise<void>,
): Promise<void> {
  if (pending.value) return;
  pending.value = true;
  failure.value = null;
  notice.value = null;
  try {
    const result = await withCsrf(send);
    if (result.kind === 'ok') {
      await onDone(result.value);
      return;
    }
    if (result.failure === 'session-lost') {
      await navigateTo('/login');
      return;
    }
    failure.value = result.failure;
    if (
      result.failure === 'session-ended'
      || result.failure === 'connection-changed'
    ) {
      await refreshState();
    }
  } finally {
    pending.value = false;
  }
}

function connect(): Promise<void> {
  return perform(start, async (value) => {
    const url = validateAuthorizeUrl('github', value);
    if (url === null) {
      failure.value = 'failed';
      return;
    }
    await navigateTo(url, { external: true });
  });
}

function done(): Promise<void> {
  return perform(endSession, refreshState);
}

function disconnect(): Promise<void> {
  const current = view.value;
  if (current === null) return Promise.resolve();
  return perform(
    disconnectAccount(current.generation, crypto.randomUUID()),
    refreshState,
  );
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
          data-testid="evidence-disconnect"
          :disabled="!csrfToken || pending"
          type="button"
          variant="destructive"
          @click="disconnect"
        >
          {{ copy.disconnect }}
        </Button>
      </div>
    </template>
    <template v-else-if="view">
      <p class="text-sm">
        {{ copy.notConnected }}
      </p>
      <div>
        <Button
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
