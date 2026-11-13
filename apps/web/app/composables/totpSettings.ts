import { encode } from 'uqr';

import type { AuthProvider } from './useAuth';
import { useAuth } from './useAuth';
import { mapReauthError, mapReauthStartError } from './passwordSettings';
import { validateAuthorizeUrl } from './providerAuthorization';
import type { components } from '../api/generated/openapi';

export type TotpEnrollmentStart
  = components['schemas']['TOTPEnrollmentStartResponse']['data'];

export type TotpEnrollmentComplete
  = components['schemas']['TOTPEnrollmentCompleteResponse']['data'];

export type TotpSettingsErrorKind
  = | 'reauth-required'
    | 'closed'
    | 'expired'
    | 'invalid-code'
    | 'not-found'
    | 'rate-limited'
    | 'unavailable';

export class TotpSettingsFailure extends Error {
  readonly kind: TotpSettingsErrorKind;

  constructor(kind: TotpSettingsErrorKind) {
    super(`totp settings failed: ${kind}`);
    this.name = 'TotpSettingsFailure';
    this.kind = kind;
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

export function mapTotpEnrollmentStartError(
  error: unknown,
): TotpSettingsFailure {
  const status = failureStatus(error);
  const code = failureCode(error);
  if (status === 403 && code === 'reauth_required') {
    return new TotpSettingsFailure('reauth-required');
  }
  if (status === 404) return new TotpSettingsFailure('closed');
  if (status === 429 && code === 'rate_limited') {
    return new TotpSettingsFailure('rate-limited');
  }
  return new TotpSettingsFailure('unavailable');
}

export function mapTotpEnrollmentCompleteError(
  error: unknown,
): TotpSettingsFailure {
  const status = failureStatus(error);
  const code = failureCode(error);
  if (status === 403 && code === 'reauth_required') {
    return new TotpSettingsFailure('reauth-required');
  }
  if (status === 404) return new TotpSettingsFailure('closed');
  if (status === 400 && code === 'enrollment_invalid') {
    return new TotpSettingsFailure('expired');
  }
  if (status === 401 && code === 'verification_failed') {
    return new TotpSettingsFailure('invalid-code');
  }
  if (status === 429 && code === 'rate_limited') {
    return new TotpSettingsFailure('rate-limited');
  }
  return new TotpSettingsFailure('unavailable');
}

export function mapTotpRemovalError(error: unknown): TotpSettingsFailure {
  const status = failureStatus(error);
  const code = failureCode(error);
  if (status === 403 && code === 'reauth_required') {
    return new TotpSettingsFailure('reauth-required');
  }
  if (status === 404) return new TotpSettingsFailure('not-found');
  if (status === 429 && code === 'rate_limited') {
    return new TotpSettingsFailure('rate-limited');
  }
  return new TotpSettingsFailure('unavailable');
}

export interface TotpQr {
  readonly size: number;
  readonly path: string;
}

export function renderTotpQr(provisioningUri: string): TotpQr {
  const grid = encode(provisioningUri, { ecc: 'M' });
  const segments: string[] = [];
  for (let y = 0; y < grid.size; y += 1) {
    const row = grid.data[y];
    for (let x = 0; x < grid.size; x += 1) {
      if (row?.[x]) segments.push(`M${x} ${y}h1v1h-1z`);
    }
  }
  return { size: grid.size, path: segments.join('') };
}

export interface TotpSettingsActions {
  reauthenticate(password: string): Promise<void>;
  startProviderReauth(provider: AuthProvider): Promise<void>;
  startEnrollment(): Promise<TotpEnrollmentStart>;
  completeEnrollment(
    enrollmentId: string,
    code: string,
  ): Promise<TotpEnrollmentComplete>;
  removeTotp(): Promise<void>;
}

interface PasswordReauthEnvelope {
  data?: { secondFactorRequired?: boolean };
}

export function useTotpSettingsActions(): TotpSettingsActions {
  const auth = useAuth();

  async function reauthenticate(password: string): Promise<void> {
    let response: PasswordReauthEnvelope | undefined;
    try {
      response = await auth.mutate<PasswordReauthEnvelope>(
        '/api/v1/auth/password/reauth',
        { method: 'POST', body: { password } },
      );
    } catch (error) {
      throw mapReauthError(error);
    }
    if (response?.data?.secondFactorRequired === true) {
      await navigateTo('/login/second-factor');
    }
  }

  async function startProviderReauth(provider: AuthProvider): Promise<void> {
    try {
      const response = await auth.mutate<{ data: { authorizeUrl: string } }>(
        `/api/v1/auth/${provider}/start`,
        { method: 'POST', query: { purpose: 'reauth' } },
      );
      const url = validateAuthorizeUrl(provider, response?.data?.authorizeUrl);
      if (!url) throw new Error('invalid OAuth authorize URL');
      await navigateTo(url, { external: true });
    } catch (error) {
      throw mapReauthStartError(error);
    }
  }

  async function startEnrollment(): Promise<TotpEnrollmentStart> {
    try {
      const response = await auth.mutate<{ data: TotpEnrollmentStart }>(
        '/api/v1/me/second-factor/totp/enrollment',
        { method: 'POST', body: {} },
      );
      return response.data;
    } catch (error) {
      throw mapTotpEnrollmentStartError(error);
    }
  }

  async function completeEnrollment(
    enrollmentId: string,
    code: string,
  ): Promise<TotpEnrollmentComplete> {
    try {
      const response = await auth.mutate<{ data: TotpEnrollmentComplete }>(
        '/api/v1/me/second-factor/totp/enrollment',
        { method: 'PUT', body: { enrollmentId, code } },
      );
      return response.data;
    } catch (error) {
      throw mapTotpEnrollmentCompleteError(error);
    }
  }

  async function removeTotp(): Promise<void> {
    try {
      await auth.mutate('/api/v1/me/second-factor/totp', { method: 'DELETE' });
    } catch (error) {
      throw mapTotpRemovalError(error);
    }
  }

  return {
    reauthenticate,
    startProviderReauth,
    startEnrollment,
    completeEnrollment,
    removeTotp,
  };
}
