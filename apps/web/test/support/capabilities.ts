import { registerEndpoint } from '@nuxt/test-utils/runtime';
import { setResponseStatus } from 'h3';

export interface CapabilityFlags {
  providerLogin: boolean;
  agentAccess: boolean;
  providers?: readonly string[];
}

/** Registers GET /api/v1/capabilities; null makes it fail with 500. */
export function registerCapabilities(
  flags: CapabilityFlags | null = { providerLogin: true, agentAccess: true },
): void {
  registerEndpoint('/api/v1/capabilities', (event) => {
    if (flags === null) {
      setResponseStatus(event, 500);
      return { error: { code: 'internal', message: 'unavailable' } };
    }
    // Like the server, `providerLogin` is true exactly when `providers` is
    // non-empty; tests that omit the list get all three or none.
    const providers = flags.providers
      ?? (flags.providerLogin ? ['google', 'github', 'linkedin'] : []);
    return { data: { ...flags, providers } };
  });
}
