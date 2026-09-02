import type { ComputedRef } from 'vue';
import type { components } from '../api/generated/openapi';

export type Capabilities = components['schemas']['Capabilities'];

interface CapabilitiesEnvelope {
  data: Capabilities;
}

export interface UseCapabilitiesReturn {
  providerLogin: ComputedRef<boolean>;
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
  const agentAccess = computed(() => data.value?.data?.agentAccess === true);
  const resolved = computed(
    () => status.value === 'success'
      || status.value === 'error'
      || error.value !== null,
  );
  return { providerLogin, agentAccess, resolved };
}
