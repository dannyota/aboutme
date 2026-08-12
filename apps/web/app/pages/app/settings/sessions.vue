<script setup lang="ts">
import type { AuthProvider } from '../../../composables/useAuth';

interface SessionInfo {
  id: string;
  createdAt: string;
  lastSeenAt: string;
  // Both fields are nullable in OpenAPI and the Go response.
  ua: string | null;
  ip: string | null;
  current: boolean;
}

interface SessionsEnvelope {
  data: SessionInfo[];
}

interface AuthStartEnvelope {
  data: {
    authorizeUrl: string;
  };
}

const allProviders: AuthProvider[] = ['google', 'github', 'linkedin'];

const route = useRoute();
const {
  csrfToken,
  identities,
  logout,
  mutate,
  refresh: refreshMe,
} = useAuth();

// Server-side rendering has neither the browser cookies nor the local proxy.
const { data: sessionsResponse } = await useFetch<SessionsEnvelope>(
  '/api/v1/sessions',
  { credentials: 'include', server: false },
);

// Mutations refresh through the browser rather than rerunning the initial
// useFetch request.
const sessionsOverride = ref<SessionInfo[] | null>(null);
const sessions = computed(
  () => sessionsOverride.value ?? sessionsResponse.value?.data ?? [],
);

const revokeError = ref<string | null>(null);

async function refreshSessions(): Promise<void> {
  const response = await $fetch<SessionsEnvelope>('/api/v1/sessions', {
    credentials: 'include',
    cache: 'no-store',
  });
  sessionsOverride.value = response.data;
}

function hasErrorCode(error: unknown, code: string): boolean {
  const actual = (
    error as { data?: { error?: { code?: string } } }
  )?.data?.error?.code;
  return actual === code;
}

function isNotFound(error: unknown): boolean {
  // The no-oracle contract makes every absent or unauthorized target the same.
  return (
    typeof error === 'object'
    && error !== null
    && 'statusCode' in error
    && (error as { statusCode?: number }).statusCode === 404
  );
}

// Reauthentication can come from a callback or a session mutation.

type ReauthReason = 'link' | 'action';

const reauthMessages: Record<ReauthReason, string> = {
  link: 'Sign in again to confirm it\'s you before we link a new '
    + 'provider.',
  action: 'Sign in again to confirm it\'s you, then try again.',
};

const reauthRequired = ref(route.query.error === 'reauth_required');
const reauthReason = ref<ReauthReason>(
  route.query.error === 'reauth_required' ? 'link' : 'action',
);
const reauthMessage = computed(() => reauthMessages[reauthReason.value]);
const reauthProvider = computed(() => identities.value[0]?.provider ?? null);

function triggerReauthPrompt(reason: ReauthReason): void {
  reauthRequired.value = true;
  reauthReason.value = reason;
}

async function revokeSession(id: string): Promise<void> {
  revokeError.value = null;
  try {
    await mutate(`/api/v1/sessions/${id}`, { method: 'DELETE' });
  } catch (error) {
    if (hasErrorCode(error, 'reauth_required')) {
      triggerReauthPrompt('action');
      return;
    }
    if (!isNotFound(error)) {
      revokeError.value = 'Could not revoke that session. Try again.';
      return;
    }
    // An already-absent session means the stale list can be refreshed.
  }
  await refreshSessions();
}

async function revokeAll(): Promise<void> {
  revokeError.value = null;
  try {
    await mutate('/api/v1/sessions', { method: 'DELETE' });
  } catch (error) {
    if (hasErrorCode(error, 'reauth_required')) {
      // Nothing was revoked, so keep the page state and request reauth.
      triggerReauthPrompt('action');
      return;
    }
    revokeError.value = 'Could not log out everywhere. Try again.';
    return;
  }
  // This also destroys the current session, so there is nothing to refresh.
  await navigateTo('/login');
}

const linkedProviders = computed(
  () => new Set(identities.value.map((i) => i.provider)),
);
const unlinkedProviders = computed(() =>
  allProviders.filter((p) => !linkedProviders.value.has(p)),
);

const showAddProvider = ref(false);
const startPending = ref(false);
const startError = ref<string | null>(null);

async function openAddProvider(): Promise<void> {
  // Refresh identities/csrfToken before offering link targets, so we don't
  // act on stale state.
  await refreshMe();
  showAddProvider.value = true;
}

const providerAuthorizeEndpoints: Record<AuthProvider, string> = {
  google: 'https://accounts.google.com/o/oauth2/v2/auth',
  github: 'https://github.com/login/oauth/authorize',
  linkedin: 'https://www.linkedin.com/oauth/v2/authorization',
};

function isLoopbackHostname(hostname: string): boolean {
  return hostname === 'localhost'
    || hostname === '127.0.0.1'
    || hostname === '[::1]';
}

function authorizeURL(
  provider: AuthProvider,
  value: unknown,
): string | null {
  if (typeof value !== 'string') return null;

  let candidate: URL;
  try {
    candidate = new URL(value);
  } catch {
    return null;
  }

  if (candidate.username || candidate.password || candidate.hash) return null;

  if (candidate.protocol === 'https:') {
    const expected = new URL(providerAuthorizeEndpoints[provider]);
    return candidate.origin === expected.origin
      && candidate.pathname === expected.pathname
      ? candidate.href
      : null;
  }

  const current = new URL(window.location.href);
  const localUAT = candidate.protocol === 'http:'
    && current.protocol === 'http:'
    && isLoopbackHostname(current.hostname)
    && candidate.origin === current.origin
    && candidate.pathname.startsWith('/__uat/oauth/');
  return localUAT ? candidate.href : null;
}

async function startOAuth(
  provider: AuthProvider,
  purpose: 'link' | 'reauth',
): Promise<void> {
  startError.value = null;
  startPending.value = true;
  try {
    const response = await mutate<AuthStartEnvelope>(
      `/api/v1/auth/${provider}/start`,
      { method: 'POST', query: { purpose } },
    );
    const url = authorizeURL(provider, response?.data?.authorizeUrl);
    if (!url) throw new Error('invalid OAuth authorize URL');
    await navigateTo(url, { external: true });
  } catch {
    startError.value = 'Something went wrong. Please try again.';
  } finally {
    startPending.value = false;
  }
}

// OAuthCallbackErrorCode in OpenAPI is the closed callback vocabulary.
const linkErrorMessages: Record<string, string> = {
  auth_failed: 'Something went wrong. Please try again.',
  cancelled: 'That was cancelled.',
  identity_already_linked: 'That provider is already linked to a '
    + 'different aboutme account.',
};

const linkErrorCode = computed(() => {
  const value = route.query.error;
  if (typeof value !== 'string' || value === 'reauth_required') return null;
  return value;
});

const linkErrorMessage = computed(() => {
  if (startError.value) return startError.value;
  if (!linkErrorCode.value) return null;
  if (Object.hasOwn(linkErrorMessages, linkErrorCode.value)) {
    return linkErrorMessages[linkErrorCode.value];
  }
  return linkErrorMessages.auth_failed;
});
</script>

<template>
  <section class="sessions">
    <h1>Signed-in devices</h1>

    <p
      v-if="revokeError"
      data-testid="revoke-error"
      role="alert"
    >
      {{ revokeError }}
    </p>

    <p
      v-if="linkErrorMessage"
      data-testid="link-error"
      role="alert"
    >
      {{ linkErrorMessage }}
    </p>

    <p
      v-if="reauthRequired && reauthProvider"
      data-testid="reauth-prompt"
      role="alert"
    >
      {{ reauthMessage }}
      <button
        type="button"
        :disabled="!csrfToken || startPending"
        @click="startOAuth(reauthProvider, 'reauth')"
      >
        Sign in again with {{ reauthProvider }}
      </button>
    </p>

    <ul>
      <li
        v-for="session in sessions"
        :key="session.id"
        :data-testid="`session-row-${session.id}`"
      >
        <span>{{ session.ua }}</span>
        <span>Last seen {{ session.lastSeenAt }}</span>

        <template v-if="session.current">
          <span>This device</span>
          <button
            type="button"
            :disabled="!csrfToken"
            @click="logout"
          >
            Log out
          </button>
        </template>
        <button
          v-else
          type="button"
          data-testid="revoke-button"
          :disabled="!csrfToken"
          @click="revokeSession(session.id)"
        >
          Revoke
        </button>
      </li>
    </ul>

    <button
      type="button"
      data-testid="revoke-all-button"
      :disabled="!csrfToken"
      @click="revokeAll"
    >
      Log out everywhere
    </button>

    <section
      v-if="unlinkedProviders.length"
      class="add-provider"
    >
      <button
        v-if="!showAddProvider"
        type="button"
        data-testid="add-provider-button"
        @click="openAddProvider"
      >
        Add another sign-in provider
      </button>

      <ul v-else>
        <li
          v-for="provider in unlinkedProviders"
          :key="provider"
        >
          <button
            type="button"
            :disabled="!csrfToken || startPending"
            @click="startOAuth(provider, 'link')"
          >
            Link {{ provider }}
          </button>
        </li>
      </ul>
    </section>
  </section>
</template>
