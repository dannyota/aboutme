<script setup lang="ts">
import type { AuthProvider } from '../../../composables/useAuth';
import PasswordSettings from '../../../components/auth/PasswordSettings.vue';
import ConnectedAgents from '../../../components/settings/ConnectedAgents.vue';
import LinkedIdentities from '@/components/settings/LinkedIdentities.vue';
import PrivacySettings from '../../../components/settings/PrivacySettings.vue';
import SecondFactorSettings
  from '../../../components/settings/SecondFactorSettings.vue';
import StatusBanner from '../../../components/app/StatusBanner.vue';
import { Button } from '../../../components/ui/button';
import {
  type PasswordSettingsActions,
  PasswordSettingsActionsKey,
  mapReauthError,
  mapReauthStartError,
  mapSetPasswordError,
} from '../../../composables/passwordSettings';
import {
  createAccountExportController,
  mapAccountDeletionError,
  PrivacySettingsActionsKey,
  type PrivacySettingsActions,
} from '../../../composables/privacySettings';
import {
  type LinkedIdentityActions,
  mapUnlinkError,
} from '../../../composables/identitySettings';
import {
  type PasskeyRegistrationCredential,
  type RegistrationCompletionResult,
  type RegistrationOptionsResult,
  type SecondFactorSettingsActions,
  SecondFactorSettingsActionsKey,
} from '../../../composables/secondFactorSettings';
import {
  providerNames,
  useCapabilities,
} from '../../../composables/useCapabilities';
import { useNow } from '../../../composables/useNow';
import {
  validateAuthorizeUrl,
} from '../../../composables/providerAuthorization';
import { goToPath } from '@/utils/navigate';
import { formatRelativeTime } from '../../../utils/relativeTime';
import { describeUserAgent } from '../../../utils/userAgent';
import { workspaceTitles } from '@/i18n/meta';
import { settingsCopy } from '@/i18n/settings';

const { locale } = useLocale();
const copy = computed(() => settingsCopy[locale.value]);

useHead({ title: computed(() => workspaceTitles[locale.value].settings) });

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
// Only providers the server enables (ADR 0039) get link or reauth controls;
// a disabled provider's start route answers not found.
const { loginProviders, agentAccess, passkeyEnrollment } = useCapabilities();
const enabledIdentities = computed(() =>
  identities.value.filter((identity) =>
    loginProviders.value.includes(identity.provider)));

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

type RevokeError = 'single' | 'all';

const revokeError = ref<RevokeError | null>(null);
const revokeErrorMessage = computed(() => {
  if (revokeError.value === 'single') return copy.value.revokeFailed;
  if (revokeError.value === 'all') return copy.value.logOutEverywhereFailed;
  return null;
});

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

const reauthRequired = ref(route.query.error === 'reauth_required');
const reauthReason = ref<ReauthReason>(
  route.query.error === 'reauth_required' ? 'link' : 'action',
);
const reauthMessage = computed(() =>
  reauthReason.value === 'link'
    ? copy.value.reauthLink
    : copy.value.reauthAction);
const reauthProvider = computed(
  () => enabledIdentities.value[0]?.provider ?? null,
);

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
      revokeError.value = 'single';
      return;
    }
    // An already-absent session means the stale list can be refreshed.
  }
  await refreshSessions();
}

// Unlinking keeps every session signed in. If the unlink was a response to a
// compromised provider account, the person can end the other sessions here,
// one existing revoke at a time, while this device stays signed in.
const unlinkedNotice = ref<string | null>(null);
const otherSessions = computed(() =>
  sessions.value.filter((session) => !session.current));
const signOutOthersPending = ref(false);

async function signOutOtherDevices(): Promise<void> {
  if (signOutOthersPending.value) return;
  signOutOthersPending.value = true;
  try {
    for (const session of otherSessions.value) {
      await revokeSession(session.id);
      if (revokeError.value !== null || reauthRequired.value) return;
    }
    unlinkedNotice.value = null;
  } finally {
    signOutOthersPending.value = false;
  }
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
    revokeError.value = 'all';
    return;
  }
  // This also destroys the current session, so there is nothing to refresh.
  await goToPath('/login');
}

const linkedProviders = computed(
  () => new Set(identities.value.map((i) => i.provider)),
);
const unlinkedProviders = computed(() =>
  loginProviders.value.filter((p) => !linkedProviders.value.has(p)),
);

const showAddProvider = ref(false);
const startPending = ref(false);
const startError = ref(false);

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
  startError.value = false;
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
    startError.value = true;
  } finally {
    startPending.value = false;
  }
}

interface PasswordReauthEnvelope {
  data?: { secondFactorRequired?: boolean };
}

async function reauthenticatePassword(password: string): Promise<void> {
  let response: PasswordReauthEnvelope | undefined;
  try {
    response = await mutate<PasswordReauthEnvelope>(
      '/api/v1/auth/password/reauth',
      { method: 'POST', body: { password } },
    );
  } catch (error) {
    throw mapReauthError(error);
  }
  // An enrolled account's primary reauthentication is accepted but not
  // complete: the pending second factor lives on its own page, whose return
  // path for this purpose is fixed back to these settings.
  if (response?.data?.secondFactorRequired === true) {
    await goToPath('/login/second-factor');
  }
}

async function startProviderReauth(provider: AuthProvider): Promise<void> {
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
}

// The password-settings actions stay closed: each performs one operation and
// rejects with a PasswordSettingsFailure rather than a raw server body. The
// provider round trip reuses the same authorizeURL validation as linking.
const passwordProviders = computed(() =>
  enabledIdentities.value.map((identity) => identity.provider),
);

const passwordActions: PasswordSettingsActions = {
  reauthenticate: reauthenticatePassword,
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
  startProviderReauth,
};

provide(PasswordSettingsActionsKey, passwordActions);

const identityActions: LinkedIdentityActions = {
  async unlink(id) {
    try {
      await mutate(`/api/v1/me/identities/${encodeURIComponent(id)}`, {
        method: 'DELETE',
      });
    } catch (error) {
      throw mapUnlinkError(error);
    }
  },
  reauthenticate: reauthenticatePassword,
  startProviderReauth,
};

const exportController = createAccountExportController();
onBeforeUnmount(() => exportController.dispose());

const privacyActions: PrivacySettingsActions = {
  exportAccount: exportController.download,
  async deleteAccount() {
    try {
      await mutate('/api/v1/me', { method: 'DELETE' });
    } catch (error) {
      throw mapAccountDeletionError(error);
    }
  },
  reauthenticate: reauthenticatePassword,
  startProviderReauth,
};

provide(PrivacySettingsActionsKey, privacyActions);

const secondFactorActions: SecondFactorSettingsActions = {
  reauthenticate: reauthenticatePassword,
  startProviderReauth,
  async registrationOptions() {
    const response = await mutate<{ data: RegistrationOptionsResult }>(
      '/api/v1/me/second-factor/passkeys/options',
      { method: 'POST', body: {} },
    );
    return response.data;
  },
  async completeRegistration(
    ceremonyId: string,
    credential: PasskeyRegistrationCredential,
  ) {
    const response = await mutate<{ data: RegistrationCompletionResult }>(
      '/api/v1/me/second-factor/passkeys',
      { method: 'POST', body: { ceremonyId, credential } },
    );
    return response.data;
  },
  async removePasskey(id: string) {
    await mutate(
      `/api/v1/me/second-factor/passkeys/${encodeURIComponent(id)}`,
      { method: 'DELETE' },
    );
  },
  async regenerateRecoveryCodes() {
    const response = await mutate<{ data: { recoveryCodes: string[] } }>(
      '/api/v1/me/second-factor/recovery-codes',
      { method: 'POST', body: {} },
    );
    return response.data.recoveryCodes;
  },
};

provide(SecondFactorSettingsActionsKey, secondFactorActions);

function onAccountDeleted(): void {
  // A top-level navigation discards every in-memory account projection before
  // the now-anonymous login page starts.
  if (import.meta.client) window.location.assign('/login');
}

async function onPasswordUpdated(): Promise<void> {
  // A successful add/change replaces the current session: refetch /me (to
  // flip hasPassword) and the device list (every other session is gone).
  await refreshMe();
  await refreshSessions();
}

async function onSecondFactorChanged(): Promise<void> {
  // A passkey add/remove or recovery-code regeneration rotates the epoch:
  // refetch /me for the replacement CSRF token and the device list, since
  // every other session and connected-agent grant is now revoked.
  await refreshMe();
  await refreshSessions();
}

// OAuthCallbackErrorCode in OpenAPI is the closed callback vocabulary.
const linkErrorCode = computed(() => {
  const value = route.query.error;
  if (typeof value !== 'string' || value === 'reauth_required') return null;
  return value;
});

const linkErrorMessage = computed(() => {
  if (startError.value) return copy.value.genericError;
  if (!linkErrorCode.value) return null;
  if (linkErrorCode.value === 'cancelled') return copy.value.cancelled;
  if (linkErrorCode.value === 'identity_already_linked') {
    return copy.value.identityAlreadyLinked;
  }
  return copy.value.genericError;
});
</script>

<template>
  <main
    class="mx-auto w-full max-w-3xl px-6 py-10"
    data-testid="settings-page"
  >
    <h1 class="text-xl font-semibold">
      {{ copy.title }}
    </h1>

    <section
      aria-labelledby="devices-title"
      class="border-t py-8"
    >
      <h2
        id="devices-title"
        class="text-lg font-semibold"
      >
        {{ copy.devices }}
      </h2>
      <StatusBanner
        v-if="revokeError"
        kind="error"
        testid="revoke-error"
      >
        {{ revokeErrorMessage }}
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
              :title="session.ua ?? copy.unknownDevice"
            >{{ describeUserAgent(session.ua ?? '', copy.userAgent) }}</span>
            <span
              v-if="session.current"
              class="ml-2 text-xs text-muted-foreground"
            >
              {{ copy.currentDevice }}
            </span>
            <span
              class="block text-xs text-muted-foreground tabular-nums"
              data-testid="session-last-seen"
            >{{
              copy.lastSeen(formatRelativeTime(session.lastSeenAt, now, locale))
            }}</span>
          </div>
          <Button
            v-if="session.current"
            :disabled="!csrfToken"
            variant="secondary"
            @click="logout"
          >
            {{ copy.logOut }}
          </Button>
          <Button
            v-else
            data-testid="revoke-button"
            :disabled="!csrfToken"
            variant="secondary"
            @click="revokeSession(session.id)"
          >
            {{ copy.revoke }}
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
        {{ copy.logOutEverywhere }}
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
      aria-labelledby="second-factor-title"
      class="border-t py-8"
    >
      <SecondFactorSettings
        :enrollment-open="passkeyEnrollment"
        :has-password="user?.hasPassword ?? false"
        :providers="passwordProviders"
        :sessions-notice="copy.secondFactorEndsOtherSessions"
        @changed="onSecondFactorChanged"
      />
    </section>

    <PrivacySettings
      class="border-t py-8"
      :has-password="user?.hasPassword ?? false"
      :providers="passwordProviders"
      @deleted="onAccountDeleted"
    />

    <section
      v-if="agentAccess"
      aria-labelledby="agents-title"
      class="border-t py-8"
    >
      <ConnectedAgents />
    </section>

    <section
      v-if="identities.length > 0 || unlinkedProviders.length > 0"
      aria-labelledby="providers-title"
      class="border-t py-8"
    >
      <h2
        id="providers-title"
        class="text-lg font-semibold"
      >
        {{ copy.providers }}
      </h2>
      <StatusBanner
        v-if="linkErrorMessage"
        kind="error"
        testid="link-error"
      >
        {{ linkErrorMessage }}
      </StatusBanner>
      <!-- Every linked identity is listed, even one whose provider is off. -->
      <LinkedIdentities
        v-if="identities.length"
        :actions="identityActions"
        class="mt-4"
        :has-password="user?.hasPassword ?? false"
        :identities="identities"
        :login-providers="loginProviders"
        @changed="refreshMe"
        @unlinked="(name) => (unlinkedNotice = name)"
      />
      <StatusBanner
        v-if="unlinkedNotice"
        class="mt-4"
        kind="success"
        testid="unlink-success"
      >
        {{ copy.unlinked(unlinkedNotice) }} {{ copy.sessionsRemain }}
        <Button
          v-if="otherSessions.length"
          class="mt-2"
          data-testid="unlink-sign-out-others"
          :disabled="!csrfToken || signOutOthersPending"
          size="sm"
          type="button"
          variant="outline"
          @click="signOutOtherDevices"
        >
          {{ copy.signOutOtherDevices }}
        </Button>
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
          {{ copy.signInAgainWith(providerNames[reauthProvider]) }}
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
          {{ copy.addProvider }}
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
              {{ copy.linkProvider(providerNames[provider]) }}
            </Button>
          </li>
        </ul>
      </div>
    </section>
  </main>
</template>
