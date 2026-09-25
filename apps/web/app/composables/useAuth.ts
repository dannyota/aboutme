import { nextTick, type ComputedRef } from 'vue';
import {
  isLocale,
  localeCookie,
  localeCookieMaxAgeSeconds,
  type Locale,
} from '@/i18n/locale';

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
  id: string;
  provider: AuthProvider;
  /** RFC 3339 UTC time the identity was linked. */
  createdAt: string;
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

/** The browser's current interface language choice, if it holds one. */
function readLocaleCookie(): Locale | undefined {
  const prefix = `${localeCookie}=`;
  const value = document.cookie
    .split('; ')
    .find((entry) => entry.startsWith(prefix))
    ?.slice(prefix.length);
  return isLocale(value) ? value : undefined;
}

/**
 * Logout, logout-everywhere, revoking the caller's own session, and account
 * deletion all send `Clear-Site-Data: "cookies", "storage"`
 * (docs/design/security.md, session lifecycle). The browser wipes every
 * cookie for this origin, including `aboutme-locale`, before the response
 * reaches `mutate`'s caller, so the next page load would fall back to
 * Vietnamese. Chromium does not expose `Clear-Site-Data` to page scripts, so
 * the response cannot say whether it cleared anything. Instead, when a
 * browser held a language choice as the request started and holds none once
 * a successful response arrives, the choice is rewritten from the in-memory
 * locale state that Clear-Site-Data cannot touch. That keeps the interface
 * language a device preference (docs/design/localization.md, "Two language
 * domains"). Writing it before the caller navigates also keeps the next
 * page's `useCookie` readers from finding no cookie and deleting it again.
 * `useCookie` can skip writing an unchanged value, so this writes
 * `document.cookie` directly, with the same attributes `useLocale` sets. A
 * browser that never chose a language still gets none, and a cookie that is
 * still present, such as one another tab just changed, is left alone.
 */
function restoreLocaleCookie(chosen: Locale): void {
  if (readLocaleCookie() !== undefined) return;
  const state = useState<Locale | undefined>(localeCookie);
  const value = isLocale(state.value) ? state.value : chosen;
  document.cookie = `${localeCookie}=${value}; Path=/; `
    + `Max-Age=${localeCookieMaxAgeSeconds}; SameSite=Lax`;
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
  // Every mutation goes through here, so the `onResponse` hook below covers
  // every Clear-Site-Data endpoint without each call site repeating it.
  async function mutate<T = void>(
    url: string,
    options: MutateOptions,
  ): Promise<T> {
    const attempt = (): Promise<T> => {
      const chosen = readLocaleCookie();
      return $fetch<T, string>(url, {
        ...options,
        credentials: 'include',
        headers: csrfHeaders(csrfToken.value, options.body !== undefined),
        onResponse: ({ response }) => {
          if (response.ok && chosen !== undefined) restoreLocaleCookie(chosen);
        },
      });
    };

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
