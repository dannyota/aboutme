import type { ComputedRef } from 'vue';
import type { components } from '../api/generated/openapi';

export type Capabilities = components['schemas']['Capabilities'];

export const loginProviderIds = ['google', 'github', 'linkedin'] as const;

export type LoginProvider = (typeof loginProviderIds)[number];

/** Brand names for the English settings surface. */
export const providerNames: Record<LoginProvider, string> = {
  google: 'Google',
  github: 'GitHub',
  linkedin: 'LinkedIn',
};

interface CapabilitiesEnvelope {
  data: Capabilities;
}

export interface UseCapabilitiesReturn {
  providerLogin: ComputedRef<boolean>;
  /** Providers whose sign-in button renders, in display order. */
  loginProviders: ComputedRef<readonly LoginProvider[]>;
  agentAccess: ComputedRef<boolean>;
  resolved: ComputedRef<boolean>;
}

/**
 * `useCapabilities` — which optional surfaces this deployment enables, read
 * from `GET /api/v1/capabilities` in the browser only (Nuxt never fetches Go
 * during SSR). Anything but an exact boolean `true` is `false`, so a failed
 * or malformed read hides every optional surface.
 */
export function useCapabilities(): UseCapabilitiesReturn {
  const { data, status, error } = useFetch<CapabilitiesEnvelope>(
    '/api/v1/capabilities',
    { server: false, credentials: 'omit', cache: 'no-store' },
  );
  const providerLogin = computed(
    () => data.value?.data?.providerLogin === true,
  );
  // ADR 0039: only the providers the server lists render a button. A missing
  // or malformed list renders none, never all three.
  const loginProviders = computed<readonly LoginProvider[]>(() => {
    const listed: unknown = data.value?.data?.providers;
    return Array.isArray(listed)
      ? loginProviderIds.filter((id) => listed.includes(id))
      : [];
  });
  const agentAccess = computed(() => data.value?.data?.agentAccess === true);
  const resolved = computed(
    () => status.value === 'success'
      || status.value === 'error'
      // Nuxt 4 leaves `error` undefined, not null, until a request fails.
      || (error.value ?? null) !== null,
  );
  return { providerLogin, loginProviders, agentAccess, resolved };
}
