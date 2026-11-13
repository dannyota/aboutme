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

export function assertMeUserSatisfiesAuthUser(payload: MeOk): AuthUser | null {
  return payload.data ? payload.data.user : null;
}

export function assertHealthzIsNotVersioned(): void {
  const api = createApiClient();
  // @ts-expect-error /healthz is served from the site root, not /api/v1
  void api.GET('/healthz');
}

type SecondFactorStateData
  = components['schemas']['SecondFactorStateResponse']['data'];
type SecondFactorRegistrationOptionsData
  = components['schemas']['SecondFactorRegistrationOptionsResponse']['data'];
type SecondFactorRegistrationCompletionData
  = components['schemas']['SecondFactorRegistrationResponse']['data'];
type SecondFactorRecoveryCodesData
  = components['schemas']['SecondFactorRecoveryCodesResponse']['data'];

export function assertSecondFactorPasskeySatisfiesSettingsContract(
  passkey: SecondFactorStateData['passkeys'][number],
): SettingsSecondFactorPasskey {
  return passkey;
}

export function assertRegistrationOptionsSatisfySettingsContract(
  payload: SecondFactorRegistrationOptionsData,
): RegistrationOptionsResult {
  return payload;
}

export function assertRegistrationCompletionSatisfiesSettingsContract(
  payload: SecondFactorRegistrationCompletionData,
): RegistrationCompletionResult {
  return payload;
}

export function assertRecoveryCodesSatisfyRegenerationContract(
  payload: SecondFactorRecoveryCodesData,
): readonly string[] {
  return payload.recoveryCodes;
}

describe('generated API surface (consumer wiring)', () => {
  it('is importable through the app alias, not just by relative path', () => {
    expect(typeof createApiClient).toBe('function');
  });

  it('defaults to the same base path nuxt.config declares', () => {
    const config = useRuntimeConfig();
    expect(API_BASE_PATH).toBe(config.public.apiBase);
  });

  it('resolves its relative base path against the origin', async () => {
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
  'generated TOTP shapes (totp-second-factor-contract.md, ADR 0017)',
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
