<script setup lang="ts">
import type { AuthProvider } from '../../../composables/useAuth';

// TODO(P1.1): Replace the privileged OAuth GET anchors below with the
// CSRF-protected POST flow in docs/adr/0014-oauth-start-methods.md.

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

async function openAddProvider(): Promise<void> {
  // Refresh identities/csrfToken before offering link targets, so we don't
  // act on stale state.
  await refreshMe();
  showAddProvider.value = true;
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
      <a
        :href="
          `/api/v1/auth/${reauthProvider}/start?purpose=reauth`
        "
      >
        Sign in again with {{ reauthProvider }}
      </a>
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
          <a :href="`/api/v1/auth/${provider}/start?purpose=link`">
            Link {{ provider }}
          </a>
        </li>
      </ul>
    </section>
  </section>
</template>
