// api-contract.test.ts is the consumer half of AC-API-001.
//
// It lives in test/nuxt/ on purpose. `.nuxt/tsconfig.app.json` includes
// `app/**/*` and `test/nuxt/**/*` and nothing else from this repo, so this
// is the only place a type assertion can be checked inside the REAL Nuxt
// program — with `~` aliases and Nuxt's auto-imports resolving exactly as
// they do in app code. A type assertion written in test/*.test.ts would be
// erased unchecked by vitest, which is why test/api-client.test.ts has to
// shell out to tsc for its half.
//
// KNOWN GAP (reported with P0F, not introduced by it): `npm run typecheck`
// is currently vacuous. It runs `vue-tsc --noEmit` against a root
// tsconfig.json that is solution-style — `"files": []` plus `references`
// — and without `--build` that program contains no files at all, so it
// type-checks nothing and passes on any error. Verified by planting a
// deliberate type error under app/ and watching typecheck exit 0. The
// exported assertions below therefore compile-check only under
// `vue-tsc --build --noEmit`, which currently also surfaces a pre-existing
// error in app/composables/useAuth.ts. The runtime cases in this file do
// run today, under `make web-test`.
import { describe, expect, it } from 'vitest';
import type { paths } from '~/api/generated/openapi';
import { API_BASE_PATH, createApiClient } from '~/api/client';
import type { AuthUser } from '~/composables/useAuth';

type MeResponses = paths['/me']['get']['responses'];
type MeOk = MeResponses['200']['content']['application/json'];

/**
 * The generated `GET /me` user must remain assignable to the hand-written
 * `AuthUser` that `useAuth` exposes to every page. If the contract's
 * `User` schema changes shape, this stops compiling — instead of the app
 * silently reading a field the API no longer sends.
 */
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
