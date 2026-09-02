import { nextTick, type ComputedRef } from 'vue';

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
 * per-session revoke, revoke-all) must send it back as `X-CSRF-Token`.
 * Mutations with JSON bodies also send `Content-Type: application/json`;
 * bodiless mutations omit it. Use `mutate()` below rather than calling
 * `$fetch` directly, so the CSRF×rotation self-heal (see `mutate`'s own doc
 * comment) applies uniformly.
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
  hasPassword: boolean;
}

interface MeEnvelope {
  data: {
    user: AuthUser;
    csrfToken: string;
    identities: AuthIdentity[];
  };
}

export interface MutateOptions {
  method: 'POST' | 'PUT' | 'DELETE';
  body?: Record<string, unknown>;
  query?: Record<string, string>;
}

export interface UseAuthReturn {
  user: ComputedRef<AuthUser | null>;
  csrfToken: ComputedRef<string | null>;
  identities: ComputedRef<AuthIdentity[]>;
  authState: ComputedRef<AuthState>;
  refresh: () => Promise<void>;
  logout: () => Promise<void>;
  mutate: <T = void>(url: string, options: MutateOptions) => Promise<T>;
}

export type AuthState = 'loading' | 'authenticated' | 'anonymous' | 'error';

/** Headers for a CSRF-protected call, including JSON only with a body. */
export function csrfHeaders(
  csrfToken: string | null,
  hasJSONBody = false,
): HeadersInit {
  const headers: Record<string, string> = {
    'X-CSRF-Token': csrfToken ?? '',
  };
  if (hasJSONBody) headers['Content-Type'] = 'application/json';
  return headers;
}

/** True for a caught fetch error whose body is `{error:{code:"..."}}`. */
function hasErrorCode(error: unknown, code: string): boolean {
  const actual = (error as { data?: { error?: { code?: string } } })?.data
    ?.error?.code;
  return actual === code;
}

function isRecoveredMeEnvelope(
  value: unknown,
  userId: string,
): value is MeEnvelope {
  if (typeof value !== 'object' || value === null) return false;
  const data = (value as { data?: unknown }).data;
  if (typeof data !== 'object' || data === null) return false;
  const recovered = data as { csrfToken?: unknown; user?: unknown };
  if (typeof recovered.user !== 'object' || recovered.user === null) {
    return false;
  }
  return (recovered.user as { id?: unknown }).id === userId
    && typeof recovered.csrfToken === 'string'
    && recovered.csrfToken !== '';
}

export function useAuth(): UseAuthReturn {
  // Authenticated reads wait for the browser, where the cookie and proxy exist.
  const {
    data,
    error,
    status,
    refresh: refreshMe,
  } = useFetch<MeEnvelope>('/api/v1/me', {
    credentials: 'include',
    server: false,
  });

  // Optional-chain all the way through: an unexpected response shape (a
  // contract drift, a proxy error page, ...) must degrade to "logged out"
  // rather than throw during render.
  const user = computed(() => data.value?.data?.user ?? null);
  const csrfToken = computed(() => data.value?.data?.csrfToken ?? null);
  const identities = computed(() => data.value?.data?.identities ?? []);
  const authState = computed<AuthState>(() => {
    if (status.value === 'idle' || status.value === 'pending') return 'loading';
    if ((error.value as { statusCode?: number } | null)?.statusCode === 401) {
      return 'anonymous';
    }
    if (error.value) return 'error';
    if (data.value === null || data.value === undefined) return 'anonymous';
    return data.value?.data?.user === undefined ? 'error' : 'authenticated';
  });

  let refreshing: Promise<void> | null = null;

  async function refresh(): Promise<void> {
    if (refreshing === null) {
      refreshing = refreshMe().then(async () => {
        await nextTick();
        const currentUserId = user.value?.id;
        if (
          authState.value !== 'authenticated'
          || currentUserId === undefined
          || csrfToken.value !== null
        ) return;
        const recovered = await $fetch<unknown>('/api/v1/me', {
          cache: 'no-store',
          credentials: 'include',
        });
        if (isRecoveredMeEnvelope(recovered, currentUserId)) {
          data.value = recovered;
        }
      }).finally(() => {
        refreshing = null;
      });
    }
    await refreshing;
  }

  // Rotation can invalidate the cached CSRF token. Refresh and retry once;
  // surface a second rejection so forged or expired requests cannot loop.
  async function mutate<T = void>(
    url: string,
    options: MutateOptions,
  ): Promise<T> {
    const attempt = (): Promise<T> =>
      $fetch<T>(url, {
        ...options,
        credentials: 'include',
        headers: csrfHeaders(csrfToken.value, options.body !== undefined),
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
    data.value = undefined;
    // Logout destroys the current session server-side (and sends
    // Clear-Site-Data) — there is no session left to refetch. Leave this
    // now-signed-out page for the login screen instead.
    await navigateTo('/login');
  }

  return { user, csrfToken, identities, authState, refresh, logout, mutate };
}
