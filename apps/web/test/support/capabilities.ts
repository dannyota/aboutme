import { registerEndpoint } from '@nuxt/test-utils/runtime';
import { setResponseStatus } from 'h3';

export interface CapabilityFlags {
  providerLogin: boolean;
  agentAccess: boolean;
  providers?: readonly string[];
  passwordRegistration?: boolean;
  passkeyEnrollment?: boolean;
  totpEnrollment?: boolean;
  signInToView?: boolean;
}

export function registerCapabilities(
  flags: CapabilityFlags | null = { providerLogin: true, agentAccess: true },
): void {
  registerEndpoint('/api/v1/capabilities', (event) => {
    if (flags === null) {
      setResponseStatus(event, 500);
      return { error: { code: 'internal', message: 'unavailable' } };
    }
    const providers = flags.providers
      ?? (flags.providerLogin ? ['google', 'github', 'linkedin'] : []);
    return {
      data: {
        ...flags,
        providers,
        passwordRegistration: flags.passwordRegistration ?? true,
        passkeyEnrollment: flags.passkeyEnrollment ?? false,
        totpEnrollment: flags.totpEnrollment ?? false,
        signInToView: flags.signInToView ?? false,
      },
    };
  });
}
