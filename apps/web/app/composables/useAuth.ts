import { nextTick, type ComputedRef } from 'vue';
import {
  isLocale,
  localeCookie,
  localeCookieMaxAgeSeconds,
  type Locale,
} from '@/i18n/locale';

export type AuthProvider = 'google' | 'github' | 'linkedin';

export interface AuthIdentity {
  id: string;
  provider: AuthProvider;
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

interface MutateOptions {
  method: 'POST' | 'PUT' | 'DELETE';
  body?: Record<string, unknown>;
  query?: Record<string, string>;
  onStatus?: (status: number) => void;
}

export interface UseAuthReturn {
  user: ComputedRef<AuthUser | null>;
  csrfToken: ComputedRef<string | null>;
  identities: ComputedRef<AuthIdentity[]>;
  authState: ComputedRef<AuthState>;
  retryAfterMs: ComputedRef<number | null>;
  refresh: () => Promise<void>;
  logout: () => Promise<void>;
  mutate: <T = void>(url: string, options: MutateOptions) => Promise<T>;
}

export type AuthState = 'loading' | 'authenticated' | 'anonymous' | 'error';

function csrfHeaders(
  csrfToken: string | null,
  hasJSONBody = false,
): HeadersInit {
  const headers: Record<string, string> = {
    'X-CSRF-Token': csrfToken ?? '',
  };
  if (hasJSONBody) headers['Content-Type'] = 'application/json';
  return headers;
}

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

function retryAfterMsOf(error: unknown): number | null {
  const value = (error as { response?: { headers?: Headers } } | null)
    ?.response?.headers?.get?.('Retry-After') ?? null;
  if (value === null || !/^[0-9]+$/.test(value)) return null;
  const seconds = Number(value);
  return Number.isSafeInteger(seconds) ? seconds * 1_000 : null;
}

function readLocaleCookie(): Locale | undefined {
  const prefix = `${localeCookie}=`;
  const value = document.cookie
    .split('; ')
    .find((entry) => entry.startsWith(prefix))
    ?.slice(prefix.length);
  return isLocale(value) ? value : undefined;
}

// Chromium hides Clear-Site-Data from page scripts, so restore the cookie.
function restoreLocaleCookie(chosen: Locale): void {
  if (readLocaleCookie() !== undefined) return;
  const state = useState<Locale | undefined>(localeCookie);
  const value = isLocale(state.value) ? state.value : chosen;
  document.cookie = `${localeCookie}=${value}; Path=/; `
    + `Max-Age=${localeCookieMaxAgeSeconds}; SameSite=Lax`;
}

export function useAuth(): UseAuthReturn {
  const {
    data,
    error,
    status,
    refresh: refreshMe,
  } = useFetch<MeEnvelope>('/api/v1/me', {
    credentials: 'include',
    server: false,
    // ofetch retries a failed GET by default; the caller owns the schedule.
    retry: 0,
  });

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
  const retryAfterMs = computed(() => retryAfterMsOf(error.value));

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
          retry: 0,
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

  async function mutate<T = void>(
    url: string,
    options: MutateOptions,
  ): Promise<T> {
    const { onStatus, ...request } = options;
    const attempt = (): Promise<T> => {
      const chosen = readLocaleCookie();
      return $fetch<T, string>(url, {
        ...request,
        credentials: 'include',
        headers: csrfHeaders(csrfToken.value, request.body !== undefined),
        onResponse: ({ response }) => {
          if (!response.ok) return;
          if (chosen !== undefined) restoreLocaleCookie(chosen);
          onStatus?.(response.status);
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
    await navigateTo('/login');
  }

  return {
    user,
    csrfToken,
    identities,
    authState,
    retryAfterMs,
    refresh,
    logout,
    mutate,
  };
}
