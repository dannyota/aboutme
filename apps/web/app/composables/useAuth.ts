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
 * alongside `Content-Type: application/json` — use `mutate()` below rather
 * than calling `$fetch` directly, so the CSRF×rotation self-heal (see
 * `mutate`'s own doc comment) applies uniformly.
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

export interface MutateOptions {
  method: 'POST' | 'DELETE';
  body?: unknown;
}

export interface UseAuthReturn {
  user: ComputedRef<AuthUser | null>;
  csrfToken: ComputedRef<string | null>;
  identities: ComputedRef<AuthIdentity[]>;
  refresh: () => Promise<void>;
  logout: () => Promise<void>;
  mutate: <T = void>(url: string, options: MutateOptions) => Promise<T>;
}

/** Headers every mutating, CSRF-protected call must send. */
export function csrfHeaders(csrfToken: string | null): HeadersInit {
  return {
    'X-CSRF-Token': csrfToken ?? '',
    'Content-Type': 'application/json',
  };
}

/** True for a caught fetch error whose body is `{error:{code:"..."}}`. */
function hasErrorCode(error: unknown, code: string): boolean {
  const actual = (
    error as { data?: { error?: { code?: string } } }
  )?.data?.error?.code;
  return actual === code;
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

  /**
   * Every CSRF-protected mutating call goes through here rather than a
   * bare `$fetch`.
   *
   * CSRF×rotation (owner ruling): a session's token rotates after 24h,
   * and the rotating response carries both the new cookie and a fresh
   * CSRF secret — so a client still holding the pre-rotation token gets
   * `403 csrf_rejected` on its very next mutation, once a day, through no
   * fault of its own. Refetching `/me` picks up the new secret in one
   * round trip, so this refetches once and retries once; a *second*
   * `csrf_rejected` (a genuinely forged/expired request, not rotation)
   * surfaces to the caller instead of retrying forever.
   */
  async function mutate<T = void>(
    url: string,
    options: MutateOptions,
  ): Promise<T> {
    const attempt = (): Promise<T> =>
      $fetch<T>(url, {
        ...options,
        credentials: 'include',
        headers: csrfHeaders(csrfToken.value),
      });

    try {
      return await attempt();
    } catch (error) {
      if (!hasErrorCode(error, 'csrf_rejected')) throw error;
      await refresh();
      return await attempt();
    }
  }

  async function logout(): Promise<void> {
    await mutate('/api/v1/auth/logout', { method: 'POST' });
    // Logout destroys the current session server-side (and sends
    // Clear-Site-Data) — there is no session left to refetch. Leave this
    // now-signed-out page for the login screen instead.
    await navigateTo('/login');
  }

  return { user, csrfToken, identities, refresh, logout, mutate };
}
