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
import type {
  TotpEnrollmentComplete,
  TotpEnrollmentStart,
} from '~/composables/totpSettings';

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

// --- Accepted TOTP shapes (docs/design/totp-second-factor-contract.md,
// ADR 0049)
//
// `totpSettings.ts` aliases `TotpEnrollmentStart` and `TotpEnrollmentComplete`
// straight from `components['schemas']`, so a mismatch there cannot compile
// (same reasoning as `secondFactorPending.ts`'s own generated aliases). The
// cases below pin the generated request and response shapes those aliases,
// `useCapabilities`, and `useSecondFactorState` depend on: a schema field
// renamed, retyped, or dropped upstream fails these before it reaches app
// code.

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

  it(
    'exposes the pending second-factor read at the versioned path',
    async () => {
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
    },
  );
});

describe(
  'generated TOTP shapes (totp-second-factor-contract.md, ADR 0049)',
  () => {
    it('exposes totpEnrollment on GET /capabilities', async () => {
      const client = createApiClient({
        fetch: async () =>
          new Response(
            JSON.stringify({
              data: {
                providerLogin: false,
                providers: [],
                agentAccess: false,
                passwordRegistration: true,
                passkeyEnrollment: true,
                totpEnrollment: true,
              },
            }),
            { status: 200, headers: { 'Content-Type': 'application/json' } },
          ),
      });

      const { data } = await client.GET('/capabilities');

      // Compiles only while `Capabilities` still declares `totpEnrollment`
      // as a boolean; `useCapabilities` reads this same field.
      const totpEnrollment: boolean | undefined = data?.data?.totpEnrollment;
      expect(totpEnrollment).toBe(true);
    });

    it('exposes totpEnabled on GET /me/second-factor', async () => {
      const client = createApiClient({
        fetch: async () =>
          new Response(
            JSON.stringify({
              data: {
                enabled: true,
                passkeys: [],
                totpEnabled: true,
                recoveryCodesRemaining: 10,
              },
            }),
            { status: 200, headers: { 'Content-Type': 'application/json' } },
          ),
      });

      const { data } = await client.GET('/me/second-factor');

      const totpEnabled: boolean | undefined = data?.data.totpEnabled;
      expect(totpEnabled).toBe(true);
    });

    it('lists totp in the pending second-factor method enum', () => {
      // `secondFactorPending.ts` narrows unknown strings against exactly
      // this alias; a literal assignment keeps `totp` pinned as a member
      // even though the composable itself only checks membership at
      // runtime.
      const method = 'totp' satisfies
        components['schemas']['SecondFactorPendingMethod'];
      expect(method).toBe('totp');
    });

    it(
      'returns the enrollment secret and provisioning URI on start',
      async () => {
        const start: TotpEnrollmentStart = {
          enrollmentId: 'A'.repeat(43),
          secret: 'ABCD EFGH IJKL MNOP QRST UVWX YZ23 4567',
          provisioningUri: 'otpauth://totp/aboutme.vn:user%40example.com?secret=ABCDEFGHIJKLMNOPQRSTUVWXYZ234567&issuer=aboutme.vn&algorithm=SHA1&digits=6&period=30',
          expiresAt: '2026-09-20T09:10:00Z',
        };
        let seen = '';
        let method = '';
        const client = createApiClient({
          fetch: async (request: Request) => {
            seen = request.url;
            method = request.method;
            return new Response(JSON.stringify({ data: start }), {
              status: 200,
              headers: { 'Content-Type': 'application/json' },
            });
          },
        });

        const { data } = await client.POST(
          '/me/second-factor/totp/enrollment',
          { body: {} },
        );

        expect(new URL(seen).pathname).toBe(
          '/api/v1/me/second-factor/totp/enrollment',
        );
        expect(method).toBe('POST');
        expect(data?.data).toEqual(start);
      },
    );

    it(
      'accepts the completion request and returns totpEnabled',
      async () => {
        const complete: TotpEnrollmentComplete = {
          totpEnabled: true,
          recoveryCodes: ['amr_00000-00000-00000-00000-00000-0'],
        };
        let seenBody: unknown;
        const client = createApiClient({
          fetch: async (request: Request) => {
            seenBody = await request.clone().json();
            return new Response(JSON.stringify({ data: complete }), {
              status: 200,
              headers: { 'Content-Type': 'application/json' },
            });
          },
        });

        const { data } = await client.PUT('/me/second-factor/totp/enrollment', {
          body: { enrollmentId: 'A'.repeat(43), code: '123456' },
        });

        expect(seenBody).toEqual({
          enrollmentId: 'A'.repeat(43),
          code: '123456',
        });
        // `totpEnabled` is a `true` literal in the generated response.
        expect(data?.data.totpEnabled).toBe(true);
        expect(data?.data.recoveryCodes).toEqual(complete.recoveryCodes);
      },
    );

    it(
      'accepts a six-digit code from POST /auth/second-factor/totp/verify',
      async () => {
        let seenBody: unknown;
        let seen = '';
        const client = createApiClient({
          fetch: async (request: Request) => {
            seen = request.url;
            seenBody = await request.clone().json();
            return new Response(JSON.stringify({ data: null }), {
              status: 200,
              headers: { 'Content-Type': 'application/json' },
            });
          },
        });

        await client.POST('/auth/second-factor/totp/verify', {
          body: { code: '123456' },
        });

        expect(new URL(seen).pathname).toBe(
          '/api/v1/auth/second-factor/totp/verify',
        );
        expect(seenBody).toEqual({ code: '123456' });
      },
    );

    it('exposes the removal route', async () => {
      let seen = '';
      let method = '';
      const client = createApiClient({
        fetch: async (request: Request) => {
          seen = request.url;
          method = request.method;
          return new Response(null, { status: 204 });
        },
      });

      await client.DELETE('/me/second-factor/totp');

      expect(new URL(seen).pathname).toBe('/api/v1/me/second-factor/totp');
      expect(method).toBe('DELETE');
    });
  },
);
