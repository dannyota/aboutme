// This file is included in Nuxt's TypeScript program, so its assertions check
// the same aliases and auto-imports as application code.
import { describe, expect, it } from 'vitest';
import type { paths } from '~/api/generated/openapi';
import { API_BASE_PATH, createApiClient } from '~/api/client';
import type { AuthUser } from '~/composables/useAuth';

type MeResponses = paths['/me']['get']['responses'];
type MeOk = MeResponses['200']['content']['application/json'];

// Keep the generated GET /me user assignable to useAuth's public type.
export function assertMeUserSatisfiesAuthUser(payload: MeOk): AuthUser | null {
  // `data` is optional in the generated intersection: the /me 200 `allOf`
  // refinement does not restate `required: [data]`. Narrow rather than
  // assert, so the contract's own looseness stays visible.
  return payload.data ? payload.data.user : null;
}

/** The versioned client must not expose the unversioned ops probes. */
export function assertHealthzIsNotVersioned(): void {
  const api = createApiClient();
  // @ts-expect-error /healthz is served from the site root, not /api/v1
  void api.GET('/healthz');
}

describe('generated API surface (consumer wiring)', () => {
  it('is importable through the app alias, not just by relative path', () => {
    // A relative import would still compile if the file sat anywhere;
    // resolving `~/api/client` proves it is where app code expects it.
    expect(typeof createApiClient).toBe('function');
  });

  it('defaults to the same base path nuxt.config declares', () => {
    // Two independent sources of "/api/v1" — runtimeConfig.public.apiBase,
    // which pages read, and the client default. Drift between them would
    // send typed calls somewhere Caddy does not route.
    const config = useRuntimeConfig();
    expect(API_BASE_PATH).toBe(config.public.apiBase);
  });

  it('resolves its relative base path against the origin', async () => {
    // The defaults are relative because dev and prod are one origin. This
    // is the browser-side proof that `baseUrl: '/api/v1'` really produces
    // /api/v1/me — see client.ts on why server-side callers must instead
    // pass an absolute baseUrl.
    let seen = '';
    const client = createApiClient({
      fetch: async (request: Request) => {
        seen = request.url;
        return new Response(JSON.stringify({ data: null }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        });
      },
    });

    await client.GET('/me');

    expect(new URL(seen).pathname).toBe('/api/v1/me');
  });
});
