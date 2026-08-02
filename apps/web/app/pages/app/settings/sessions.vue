<script setup lang="ts">
import type { AuthProvider } from '../../../composables/useAuth';
import { csrfHeaders } from '../../../composables/useAuth';

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
 * landing back on this page with `?error=reauth_required`. When that
 * happens we show a "confirm it's you" prompt that re-triggers
 * `purpose=reauth` against one of the user's already-linked providers
 * before the link is retried.
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
const { csrfToken, identities, logout, refresh: refreshMe } = useAuth();

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

function isNotFound(error: unknown): boolean {
  return (
    typeof error === 'object'
    && error !== null
    && 'statusCode' in error
    && (error as { statusCode?: number }).statusCode === 404
  );
}

async function revokeSession(id: string): Promise<void> {
  revokeError.value = null;
  try {
    await $fetch(`/api/v1/sessions/${id}`, {
      method: 'DELETE',
      credentials: 'include',
      headers: csrfHeaders(csrfToken.value),
    });
  } catch (error) {
    if (!isNotFound(error)) {
      revokeError.value = 'Could not revoke that session. Try again.';
      return;
    }
    // 404: already gone (DD-C5) — not an error, just a stale list.
  }
  await refreshSessions();
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

// Reachable two ways: (1) the callback landing on this page's URL with
// `?error=reauth_required` after a `purpose=link` top-level navigation
// bounced back, or (2) a same-page fetch (revoke-all below) getting a
// live `403 reauth_required` — which never touches the URL, so this has
// to be settable programmatically, not just derived from the route.
const reauthRequired = ref(route.query.error === 'reauth_required');
const reauthProvider = computed(() => identities.value[0]?.provider ?? null);

function isReauthRequiredError(error: unknown): boolean {
  const code = (
    error as { data?: { error?: { code?: string } } }
  )?.data?.error?.code;
  return code === 'reauth_required';
}

async function revokeAll(): Promise<void> {
  revokeError.value = null;
  try {
    await $fetch('/api/v1/sessions/revoke-all', {
      method: 'POST',
      credentials: 'include',
      headers: csrfHeaders(csrfToken.value),
    });
  } catch (error) {
    if (isReauthRequiredError(error)) {
      // Logout-everywhere requires recent reauth (spec: "sensitive
      // operations require recent OAuth reauth"). Nothing was revoked —
      // show the same "confirm it's you" prompt the link flow uses,
      // rather than a generic error.
      reauthRequired.value = true;
      return;
    }
    revokeError.value = 'Could not log out everywhere. Try again.';
    return;
  }
  // Success destroys the current session too (Clear-Site-Data) — there is
  // nothing left here to refetch; leave for the login screen.
  await navigateTo('/login');
}
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
      v-if="reauthRequired && reauthProvider"
      data-testid="reauth-prompt"
      role="alert"
    >
      Sign in again to confirm it's you before we link a new provider.
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
            @click="logout"
          >
            Log out
          </button>
        </template>
        <button
          v-else
          type="button"
          data-testid="revoke-button"
          @click="revokeSession(session.id)"
        >
          Revoke
        </button>
      </li>
    </ul>

    <button
      type="button"
      data-testid="revoke-all-button"
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
