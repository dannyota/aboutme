import { registerEndpoint } from '@nuxt/test-utils/runtime';
import { setResponseStatus } from 'h3';

export interface CapabilityFlags {
  providerLogin: boolean;
  agentAccess: boolean;
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
    return { data: flags };
  });
}
