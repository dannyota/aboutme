<script setup lang="ts">
import type { AuthProvider } from '../../../composables/useAuth';
import PasswordSettings from '../../../components/auth/PasswordSettings.vue';
import ConnectedAgents from '../../../components/settings/ConnectedAgents.vue';
import StatusBanner from '../../../components/app/StatusBanner.vue';
import { Button } from '../../../components/ui/button';
import {
  type PasswordSettingsActions,
  PasswordSettingsActionsKey,
  mapReauthError,
  mapReauthStartError,
  mapSetPasswordError,
} from '../../../composables/passwordSettings';
import { useCapabilities } from '../../../composables/useCapabilities';
import { useNow } from '../../../composables/useNow';
import {
  validateAuthorizeUrl,
} from '../../../composables/providerAuthorization';
import { formatRelativeTime } from '../../../utils/relativeTime';
import { describeUserAgent } from '../../../utils/userAgent';

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
const props = defineProps<{ now?: Date }>();
const now = props.now ?? useNow();

const route = useRoute();
const {
  csrfToken,
  identities,
  user,
  logout,
  mutate,
  refresh: refreshMe,
} = useAuth();
const { providerLogin, agentAccess } = useCapabilities();

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
  const actual = (error as { data?: { error?: { code?: string } } })?.data
    ?.error?.code;
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
  link: [
    'Sign in again to confirm it\'s you before we link a new',
    'provider.',
  ].join(' '),
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
    const url = validateAuthorizeUrl(provider, response?.data?.authorizeUrl);
    if (!url) throw new Error('invalid OAuth authorize URL');
    await navigateTo(url, { external: true });
  } catch (error) {
    if (purpose === 'link' && hasErrorCode(error, 'reauth_required')) {
      triggerReauthPrompt('link');
      return;
    }
    startError.value = 'Something went wrong. Please try again.';
  } finally {
    startPending.value = false;
  }
}

// The password-settings actions stay closed: each performs one operation and
// rejects with a PasswordSettingsFailure rather than a raw server body. The
// provider round trip reuses the same authorizeURL validation as linking.
const passwordProviders = computed(() =>
  providerLogin.value
    ? identities.value.map((identity) => identity.provider)
    : [],
);

const passwordActions: PasswordSettingsActions = {
  async reauthenticate(password) {
    try {
      await mutate('/api/v1/auth/password/reauth', {
        method: 'POST',
        body: { password },
      });
    } catch (error) {
      throw mapReauthError(error);
    }
  },
  async setPassword(password) {
    try {
      await mutate('/api/v1/me/password', {
        method: 'PUT',
        body: { password },
      });
    } catch (error) {
      throw mapSetPasswordError(error);
    }
  },
  async startProviderReauth(provider) {
    try {
      const response = await mutate<AuthStartEnvelope>(
        `/api/v1/auth/${provider}/start`,
        { method: 'POST', query: { purpose: 'reauth' } },
      );
      const url = validateAuthorizeUrl(provider, response?.data?.authorizeUrl);
      if (!url) throw new Error('invalid OAuth authorize URL');
      await navigateTo(url, { external: true });
    } catch (error) {
      throw mapReauthStartError(error);
    }
  },
};

provide(PasswordSettingsActionsKey, passwordActions);

async function onPasswordUpdated(): Promise<void> {
  // A successful add/change replaces the current session: refetch /me (to
  // flip hasPassword) and the device list (every other session is gone).
  await refreshMe();
  await refreshSessions();
}

// OAuthCallbackErrorCode in OpenAPI is the closed callback vocabulary.
const linkErrorMessages: Record<string, string> = {
  auth_failed: 'Something went wrong. Please try again.',
  cancelled: 'That was cancelled.',
  identity_already_linked:
    'That provider is already linked to a ' + 'different aboutme account.',
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
  <main
    class="mx-auto w-full max-w-3xl px-6 py-10"
    data-testid="settings-page"
  >
    <h1 class="text-xl font-semibold">
      Settings
    </h1>

    <section
      aria-labelledby="devices-title"
      class="border-t py-8"
    >
      <h2
        id="devices-title"
        class="text-lg font-semibold"
      >
        Signed-in devices
      </h2>
      <StatusBanner
        v-if="revokeError"
        kind="error"
        testid="revoke-error"
      >
        {{ revokeError }}
      </StatusBanner>
      <ul class="mt-4 divide-y">
        <li
          v-for="session in sessions"
          :key="session.id"
          :data-testid="`session-row-${session.id}`"
          class="grid grid-cols-[1fr_auto] gap-4 py-3"
        >
          <div>
            <span
              data-testid="session-description"
              :title="session.ua ?? 'Unknown device'"
            >{{ describeUserAgent(session.ua ?? "") }}</span>
            <span
              v-if="session.current"
              class="ml-2 text-xs text-muted-foreground"
            >
              This device
            </span>
            <span
              class="block text-xs text-muted-foreground tabular-nums"
              data-testid="session-last-seen"
            >Last seen {{ formatRelativeTime(session.lastSeenAt, now) }}</span>
          </div>
          <Button
            v-if="session.current"
            :disabled="!csrfToken"
            variant="secondary"
            @click="logout"
          >
            Log out
          </Button>
          <Button
            v-else
            data-testid="revoke-button"
            :disabled="!csrfToken"
            variant="secondary"
            @click="revokeSession(session.id)"
          >
            Revoke
          </Button>
        </li>
      </ul>
      <Button
        data-testid="revoke-all-button"
        class="mt-4 text-destructive"
        :disabled="!csrfToken"
        variant="outline"
        @click="revokeAll"
      >
        Log out everywhere
      </Button>
    </section>

    <section
      aria-labelledby="password-title"
      class="border-t py-8"
    >
      <PasswordSettings
        :has-password="user?.hasPassword ?? false"
        :providers="passwordProviders"
        @updated="onPasswordUpdated"
      />
    </section>

    <section
      v-if="agentAccess"
      aria-labelledby="agents-title"
      class="border-t py-8"
    >
      <ConnectedAgents />
    </section>

    <section
      v-if="providerLogin"
      aria-labelledby="providers-title"
      class="border-t py-8"
    >
      <h2
        id="providers-title"
        class="text-lg font-semibold"
      >
        Sign-in providers
      </h2>
      <StatusBanner
        v-if="linkErrorMessage"
        kind="error"
        testid="link-error"
      >
        {{ linkErrorMessage }}
      </StatusBanner>
      <StatusBanner
        v-if="reauthRequired && reauthProvider"
        kind="error"
        testid="reauth-prompt"
      >
        {{ reauthMessage }}
        <Button
          class="mt-2"
          :disabled="!csrfToken || startPending"
          variant="outline"
          @click="startOAuth(reauthProvider, 'reauth')"
        >
          Sign in again with {{ reauthProvider }}
        </Button>
      </StatusBanner>
      <div
        v-if="unlinkedProviders.length"
        class="mt-4"
      >
        <Button
          v-if="!showAddProvider"
          data-testid="add-provider-button"
          variant="outline"
          @click="openAddProvider"
        >
          Add another sign-in provider
        </Button>
        <ul
          v-else
          class="flex flex-wrap gap-2"
        >
          <li
            v-for="provider in unlinkedProviders"
            :key="provider"
          >
            <Button
              :disabled="!csrfToken || startPending"
              variant="outline"
              @click="startOAuth(provider, 'link')"
            >
              Link {{ provider }}
            </Button>
          </li>
        </ul>
      </div>
    </section>
  </main>
</template>
