<script setup lang="ts">
import type { AuthProvider } from '../../../composables/useAuth';

/**
 * Session device management: list, per-session revoke, logout-everywhere,
 * and a minimal reauth-gated provider-linking prompt.
 *
 * Revoking your own *current* session is logout, not a generic device
 * removal, so the current row never shows the same "Revoke" action as the
 * other rows (design decision from the task brief).
 *
 * Linking a new provider (`purpose=link`) is a real top-level navigation
 * (like the login page's provider buttons) — it is not a fetchable JSON
 * call, so a stale-reauth rejection can only be observed by the callback
 * landing back on this page with `?error=reauth_required`. `DELETE
 * /sessions/{id}` and `DELETE /sessions` (per-session revoke and
 * logout-everywhere, DD-C11) can *also* return a live `403
 * reauth_required` — the same "confirm it's you" prompt below handles
 * both triggers, with reason-specific copy (`reauthReason`) since only
 * one of them is actually about linking a provider.
 */

interface SessionInfo {
  id: string;
  createdAt: string;
  lastSeenAt: string;
  ua: string;
  ip: string;
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

// DD-C9: same reasoning as useAuth's own `/me` call — this must not run
// during SSR (no proxy, no cookies there), so the real fetch happens
// client-side only.
const { data: sessionsResponse } = await useFetch<SessionsEnvelope>(
  '/api/v1/sessions',
  { credentials: 'include', server: false },
);

// Subsequent refreshes (after a revoke/revoke-all) go through a plain
// `$fetch` into this override rather than `useFetch`'s own `refresh()`,
// which is tuned for re-running the same SSR-time request and is not the
// right tool for "refetch after a client-side mutation."
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
  // 404s carry no `{error:{code}}` body distinct from any other 404 in
  // this app (DD-C5's no-oracle contract) — the status is the signal.
  return (
    typeof error === 'object'
    && error !== null
    && 'statusCode' in error
    && (error as { statusCode?: number }).statusCode === 404
  );
}

// --- Reauth prompt (route-driven from a link/reauth redirect, or
// live-triggered by a 403 from either DELETE endpoint below) ---

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
    // 404: already gone (DD-C5) — not an error, just a stale list.
  }
  await refreshSessions();
}

async function revokeAll(): Promise<void> {
  revokeError.value = null;
  try {
    // DD-C11 (spec-corrected): logout-everywhere is DELETE /sessions —
    // there is no POST /sessions/revoke-all.
    await mutate('/api/v1/sessions', { method: 'DELETE' });
  } catch (error) {
    if (hasErrorCode(error, 'reauth_required')) {
      // Logout-everywhere requires recent reauth (spec: "sensitive
      // operations require recent OAuth reauth"). Nothing was revoked —
      // show the same "confirm it's you" prompt the link flow uses,
      // rather than a generic error.
      triggerReauthPrompt('action');
      return;
    }
    revokeError.value = 'Could not log out everywhere. Try again.';
    return;
  }
  // Success destroys the current session too (Clear-Site-Data) — there is
  // nothing left here to refetch; leave for the login screen.
  await navigateTo('/login');
}

// --- Provider linking (Step 3: minimal reauth-required UX) ---

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

// Closed vocabulary landing here from a rejected `?purpose=link`/`reauth`
// callback (DD-C15/OAuthCallbackErrorCode) — `reauth_required` is handled
// by the dedicated prompt above instead of this generic banner.
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
