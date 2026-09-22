/**
 * `secondFactorPending` — the API surface behind `/login/second-factor`:
 * reading pending status and completing it with a passkey assertion or a
 * recovery code.
 *
 * Every call sends only the `__Host-auth-pending` cookie (`credentials:
 * 'include'`, set by the browser automatically) and the pending row's own
 * CSRF token from `GET /auth/second-factor`'s `csrfToken` field. It never
 * reuses the session synchronizer token from `GET /me`, and never calls
 * `/me` at all — the pending cookie is not a session and this module treats
 * it as strictly less than one. See
 * docs/design/passkey-second-factor-contract.md.
 */
import type { components } from '../api/generated/openapi';
import type { AssertionCredentialJSON } from '../utils/webauthn';

export type SecondFactorPendingStatus
  = components['schemas']['SecondFactorPendingStatusResponse']['data'];
export type SecondFactorPendingMethod
  = components['schemas']['SecondFactorPendingMethod'];
export type WebAuthnAssertionPublicKey
  = components['schemas']['WebAuthnAssertionPublicKey'];

const KNOWN_METHODS: readonly SecondFactorPendingMethod[] = [
  'passkey',
  'recovery',
];

/** True for a method value this page knows how to complete. */
export function isKnownPendingMethod(
  value: unknown,
): value is SecondFactorPendingMethod {
  return typeof value === 'string'
    && (KNOWN_METHODS as readonly string[]).includes(value);
}

export type SecondFactorPendingError
  = | 'authentication-required'
    | 'verification-failed'
    | 'challenge-invalid'
    | 'factor-not-found'
    | 'rate-limited'
    | 'request-invalid'
    | 'csrf-rejected'
    | 'unavailable';

/** Rejection value for every pending second-factor operation. */
export class SecondFactorPendingFailure extends Error {
  readonly kind: SecondFactorPendingError;
  readonly retryAfterSeconds?: number;

  constructor(kind: SecondFactorPendingError, retryAfterSeconds?: number) {
    super(`second factor pending failed: ${kind}`);
    this.name = 'SecondFactorPendingFailure';
    this.kind = kind;
    if (retryAfterSeconds !== undefined) {
      this.retryAfterSeconds = retryAfterSeconds;
    }
  }
}

function failureStatus(error: unknown): number | null {
  const candidate = error as { statusCode?: unknown; status?: unknown } | null;
  for (const key of ['statusCode', 'status'] as const) {
    const value = candidate?.[key];
    if (typeof value === 'number') return value;
  }
  return null;
}

function failureCode(error: unknown): unknown {
  return (error as { data?: { error?: { code?: unknown } } } | null)?.data
    ?.error?.code;
}

function retryAfterSeconds(error: unknown): number | undefined {
  const headers = (error as { response?: { headers?: Headers } } | null)
    ?.response?.headers;
  const raw = headers?.get?.('Retry-After') ?? null;
  const parsed = raw === null ? Number.NaN : Number(raw);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : undefined;
}

/**
 * Maps an unknown thrown value to the closed pending-failure union. Only the
 * exact status/code pairs the contract defines match; everything else
 * degrades to `unavailable`, matching the password composable's rule that a
 * drifting or hostile server body cannot leak into the UI.
 */
export function mapSecondFactorPendingError(
  error: unknown,
): SecondFactorPendingFailure {
  const status = failureStatus(error);
  const code = failureCode(error);
  const exact = (wantStatus: number, wantCode: string): boolean =>
    status === wantStatus && code === wantCode;

  if (exact(401, 'authentication_required')) {
    return new SecondFactorPendingFailure('authentication-required');
  }
  if (exact(401, 'verification_failed')) {
    return new SecondFactorPendingFailure('verification-failed');
  }
  if (exact(400, 'challenge_invalid')) {
    return new SecondFactorPendingFailure('challenge-invalid');
  }
  if (exact(400, 'request_invalid')) {
    return new SecondFactorPendingFailure('request-invalid');
  }
  if (exact(403, 'csrf_rejected')) {
    return new SecondFactorPendingFailure('csrf-rejected');
  }
  if (exact(404, 'factor_not_found')) {
    return new SecondFactorPendingFailure('factor-not-found');
  }
  if (exact(429, 'rate_limited')) {
    return new SecondFactorPendingFailure(
      'rate-limited',
      retryAfterSeconds(error),
    );
  }
  return new SecondFactorPendingFailure('unavailable');
}

function isPendingStatus(value: unknown): value is SecondFactorPendingStatus {
  if (typeof value !== 'object' || value === null) return false;
  const candidate = value as Partial<SecondFactorPendingStatus>;
  return (candidate.purpose === 'login' || candidate.purpose === 'reauth')
    && Array.isArray(candidate.methods)
    && candidate.methods.length > 0
    && candidate.methods.every((method) => typeof method === 'string')
    && typeof candidate.expiresAt === 'string'
    && typeof candidate.returnPath === 'string'
    && typeof candidate.csrfToken === 'string'
    && candidate.csrfToken !== '';
}

export interface SecondFactorAssertionOptions {
  ceremonyId: string;
  publicKey: WebAuthnAssertionPublicKey;
}

function isAssertionOptions(
  value: unknown,
): value is SecondFactorAssertionOptions {
  if (typeof value !== 'object' || value === null) return false;
  const candidate = value as { ceremonyId?: unknown; publicKey?: unknown };
  return typeof candidate.ceremonyId === 'string'
    && candidate.ceremonyId !== ''
    && typeof candidate.publicKey === 'object'
    && candidate.publicKey !== null;
}

function pendingHeaders(csrfToken: string): HeadersInit {
  return {
    'X-CSRF-Token': csrfToken,
    'Content-Type': 'application/json',
  };
}

export interface UseSecondFactorPending {
  /** `GET /api/v1/auth/second-factor`: the pending row's own status. */
  status(): Promise<SecondFactorPendingStatus>;
  /** `POST /api/v1/auth/second-factor/passkey/options`. */
  passkeyOptions(csrfToken: string): Promise<SecondFactorAssertionOptions>;
  /** `POST /api/v1/auth/second-factor/passkey/verify`. */
  verifyPasskey(
    csrfToken: string,
    ceremonyId: string,
    credential: AssertionCredentialJSON,
  ): Promise<void>;
  /** `POST /api/v1/auth/second-factor/recovery/verify`. */
  verifyRecovery(csrfToken: string, code: string): Promise<void>;
}

export function useSecondFactorPending(): UseSecondFactorPending {
  return {
    async status() {
      let result: unknown;
      try {
        result = await $fetch('/api/v1/auth/second-factor', {
          method: 'GET',
          credentials: 'include',
          cache: 'no-store',
        });
      } catch (error) {
        throw mapSecondFactorPendingError(error);
      }
      const data = (result as { data?: unknown } | null)?.data;
      if (!isPendingStatus(data)) {
        throw new SecondFactorPendingFailure('unavailable');
      }
      return data;
    },

    async passkeyOptions(csrfToken) {
      let result: unknown;
      try {
        result = await $fetch('/api/v1/auth/second-factor/passkey/options', {
          method: 'POST',
          body: {},
          credentials: 'include',
          headers: pendingHeaders(csrfToken),
        });
      } catch (error) {
        throw mapSecondFactorPendingError(error);
      }
      const data = (result as { data?: unknown } | null)?.data;
      if (!isAssertionOptions(data)) {
        throw new SecondFactorPendingFailure('unavailable');
      }
      return data;
    },

    async verifyPasskey(csrfToken, ceremonyId, credential) {
      try {
        await $fetch('/api/v1/auth/second-factor/passkey/verify', {
          method: 'POST',
          body: { ceremonyId, credential },
          credentials: 'include',
          headers: pendingHeaders(csrfToken),
        });
      } catch (error) {
        throw mapSecondFactorPendingError(error);
      }
    },

    async verifyRecovery(csrfToken, code) {
      try {
        await $fetch('/api/v1/auth/second-factor/recovery/verify', {
          method: 'POST',
          body: { code },
          credentials: 'include',
          headers: pendingHeaders(csrfToken),
        });
      } catch (error) {
        throw mapSecondFactorPendingError(error);
      }
    },
  };
}
