import type { ComputedRef } from 'vue';

/**
 * `useAuth` — session/identity state backed by `GET /api/v1/me`.
 *
 * Same-origin in dev/prod (Caddy routes `/api/v1/*` to the Go API on the
 * same origin), so `credentials: 'include'` is mostly the browser default
 * already — stated explicitly here since it is security-relevant (the
 * cookie is `__Host-session`, httpOnly, and must travel with this call for
 * `/me` to resolve a session at all).
 *
 * `csrfToken` comes from the response body only — never a cookie, URL, or
 * log (spec: CSRF §"Sessions"). Every mutating call in this app (logout,
 * per-session revoke, revoke-all) must send it back as `X-CSRF-Token`
 * alongside `Content-Type: application/json`.
 */

export type AuthProvider = 'google' | 'github' | 'linkedin';

export interface AuthIdentity {
  provider: AuthProvider;
}

export interface AuthUser {
  id: string;
  email: string;
  name: string | null;
  avatarKey: string | null;
}

interface MeEnvelope {
  data: {
    user: AuthUser;
    csrfToken: string;
    identities: AuthIdentity[];
  };
}

export interface UseAuthReturn {
  user: ComputedRef<AuthUser | null>;
  csrfToken: ComputedRef<string | null>;
  identities: ComputedRef<AuthIdentity[]>;
  refresh: () => Promise<void>;
  logout: () => Promise<void>;
}

/** Headers every mutating, CSRF-protected call must send. */
export function csrfHeaders(csrfToken: string | null): HeadersInit {
  return {
    'X-CSRF-Token': csrfToken ?? '',
    'Content-Type': 'application/json',
  };
}

export function useAuth(): UseAuthReturn {
  // DD-C9: authenticated `/api/v1` fetches must not run during SSR — Nitro
  // has no proxy and no browser cookies at that point, so an SSR-time
  // attempt resolves against the wrong (unauthenticated) target, and
  // Nuxt's hydration guard then treats that bad result as authoritative
  // and never refetches, leaving the page permanently empty on a hard
  // load. `server: false` defers the real fetch to the client, where the
  // cookie and the Caddy proxy both exist.
  const { data, refresh: refreshMe } = useFetch<MeEnvelope>(
    '/api/v1/me',
    { credentials: 'include', server: false },
  );

  // Optional-chain all the way through: an unexpected response shape (a
  // contract drift, a proxy error page, ...) must degrade to "logged out"
  // rather than throw during render.
  const user = computed(() => data.value?.data?.user ?? null);
  const csrfToken = computed(() => data.value?.data?.csrfToken ?? null);
  const identities = computed(() => data.value?.data?.identities ?? []);

  async function refresh(): Promise<void> {
    await refreshMe();
  }

  async function logout(): Promise<void> {
    await $fetch('/api/v1/auth/logout', {
      method: 'POST',
      credentials: 'include',
      headers: csrfHeaders(csrfToken.value),
    });
    // Logout destroys the current session server-side (and sends
    // Clear-Site-Data) — there is no session left to refetch. Leave this
    // now-signed-out page for the login screen instead.
    await navigateTo('/login');
  }

  return { user, csrfToken, identities, refresh, logout };
}
