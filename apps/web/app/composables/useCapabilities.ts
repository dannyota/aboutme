import type { ComputedRef } from 'vue';
import type { components } from '../api/generated/openapi';

export type Capabilities = components['schemas']['Capabilities'];

const loginProviderIds = ['google', 'github', 'linkedin'] as const;

export type LoginProvider = (typeof loginProviderIds)[number];

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
  passwordRegistration: ComputedRef<boolean>;
  loginProviders: ComputedRef<readonly LoginProvider[]>;
  agentAccess: ComputedRef<boolean>;
  passkeyEnrollment: ComputedRef<boolean>;
  totpEnrollment: ComputedRef<boolean>;
  signInToView: ComputedRef<boolean>;
  resolved: ComputedRef<boolean>;
}

export function useCapabilities(): UseCapabilitiesReturn {
  const { data, status, error } = useFetch<CapabilitiesEnvelope>(
    '/api/v1/capabilities',
    { server: false, credentials: 'omit', cache: 'no-store' },
  );
  const providerLogin = computed(
    () => data.value?.data?.providerLogin === true,
  );
  const loginProviders = computed<readonly LoginProvider[]>(() => {
    const listed: unknown = data.value?.data?.providers;
    return Array.isArray(listed)
      ? loginProviderIds.filter((id) => listed.includes(id))
      : [];
  });
  const agentAccess = computed(() => data.value?.data?.agentAccess === true);
  const passkeyEnrollment = computed(
    () => data.value?.data?.passkeyEnrollment === true,
  );
  const totpEnrollment = computed(
    () => data.value?.data?.totpEnrollment === true,
  );
  const signInToView = computed(
    () => data.value?.data?.signInToView === true,
  );
  const passwordRegistration = computed(
    () => data.value?.data?.passwordRegistration !== false,
  );
  const resolved = computed(
    () => status.value === 'success'
      || status.value === 'error'
      // Nuxt 4 leaves `error` undefined, not null, until a request fails.
      || (error.value ?? null) !== null,
  );
  return {
    providerLogin,
    passwordRegistration,
    loginProviders,
    agentAccess,
    passkeyEnrollment,
    totpEnrollment,
    signInToView,
    resolved,
  };
}
