// This file is included in Nuxt's TypeScript program, so its assertions check
// the same aliases and auto-imports as application code.
import { describe, expect, it } from 'vitest';
import type { components, paths } from '~/api/generated/openapi';
import { API_BASE_PATH, createApiClient } from '~/api/client';
import type { AuthUser } from '~/composables/useAuth';
import type {
  RegistrationCompletionResult,
  RegistrationOptionsResult,
  SecondFactorPasskey as SettingsSecondFactorPasskey,
} from '~/composables/secondFactorSettings';

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

// --- Accepted passkey shapes (docs/design/passkey-second-factor-contract.md)
//
// `secondFactorPending.ts` and `webauthn.ts` alias their accepted shapes
// straight from `components['schemas']`, so a mismatch there cannot compile.
// `secondFactorSettings.ts` instead declares its own parallel types for the
// account-settings passkey flow; the checks below keep those hand-written
// types assignable from what the generated client actually returns, the same
// way `assertMeUserSatisfiesAuthUser` guards `useAuth`'s hand-written type.

type SecondFactorStateData
  = components['schemas']['SecondFactorStateResponse']['data'];
type SecondFactorRegistrationOptionsData
  = components['schemas']['SecondFactorRegistrationOptionsResponse']['data'];
type SecondFactorRegistrationCompletionData
  = components['schemas']['SecondFactorRegistrationResponse']['data'];
type SecondFactorRecoveryCodesData
  = components['schemas']['SecondFactorRecoveryCodesResponse']['data'];

/**
 * Keep a generated `GET /me/second-factor` passkey entry assignable to the
 * settings composable's own passkey type: `useSecondFactorState` renders
 * this list directly.
 */
export function assertSecondFactorPasskeySatisfiesSettingsContract(
  passkey: SecondFactorStateData['passkeys'][number],
): SettingsSecondFactorPasskey {
  return passkey;
}

/**
 * `SecondFactorSettingsActions.registrationOptions` declares
 * `RegistrationOptionsResult` as its return type; the generated
 * `POST /me/second-factor/passkeys/options` response must satisfy it.
 */
export function assertRegistrationOptionsSatisfySettingsContract(
  payload: SecondFactorRegistrationOptionsData,
): RegistrationOptionsResult {
  return payload;
}

/**
 * `SecondFactorSettingsActions.completeRegistration` declares
 * `RegistrationCompletionResult` as its return type; the generated
 * `POST /me/second-factor/passkeys` response must satisfy it.
 */
export function assertRegistrationCompletionSatisfiesSettingsContract(
  payload: SecondFactorRegistrationCompletionData,
): RegistrationCompletionResult {
  return payload;
}

/**
 * `SecondFactorSettingsActions.regenerateRecoveryCodes` returns
 * `Promise<readonly string[]>`; the generated
 * `POST /me/second-factor/recovery-codes` response must satisfy it.
 */
export function assertRecoveryCodesSatisfyRegenerationContract(
  payload: SecondFactorRecoveryCodesData,
): readonly string[] {
  return payload.recoveryCodes;
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

  it('exposes the pending second-factor status read at the versioned path', async () => {
    // `/login/second-factor` reads pending status through `$fetch`
    // directly (see secondFactorPending.ts), not through this generated
    // client. This proves the generated client itself carries the route at
    // the same versioned path, so a typed caller could use it too.
    let seen = '';
    const client = createApiClient({
      fetch: async (request: Request) => {
        seen = request.url;
        return new Response(
          JSON.stringify({
            data: {
              purpose: 'login',
              methods: ['passkey', 'recovery'],
              expiresAt: '2026-09-20T09:05:00Z',
              returnPath: '/app/resumes',
              csrfToken: '0'.repeat(43),
            },
          }),
          { status: 200, headers: { 'Content-Type': 'application/json' } },
        );
      },
    });

    await client.GET('/auth/second-factor');

    expect(new URL(seen).pathname).toBe('/api/v1/auth/second-factor');
  });
});
