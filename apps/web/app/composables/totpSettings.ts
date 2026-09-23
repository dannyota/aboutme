import { encode } from 'uqr';

import type { AuthProvider } from './useAuth';
import { useAuth } from './useAuth';
import { mapReauthError, mapReauthStartError } from './passwordSettings';
import { validateAuthorizeUrl } from './providerAuthorization';
import type { components } from '../api/generated/openapi';

/**
 * `totpSettings` — the closed contract behind the account-settings
 * authenticator-app (TOTP) controls
 * (`docs/design/totp-second-factor-contract.md`, ADR 0049).
 *
 * `TotpSettings` is presentational: it reads `totpEnabled` and
 * `hasOtherActiveFactor` as props, reads the `totpEnrollment` capability
 * itself, and performs every side effect through `useTotpSettingsActions`.
 * Every rejection is a `TotpSettingsFailure` so the component maps to fixed
 * copy without reading a raw server body. A `reauth-required` failure is
 * always forwarded to the parent, which owns the one shared reauthentication
 * flow for the whole second-factor section.
 */

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

/** Rejection value for every TOTP-settings action. */
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

/** Map a `POST .../totp/enrollment` (start) rejection. */
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

/**
 * Map a `PUT .../totp/enrollment` (complete) rejection. `enrollment_invalid`
 * (unknown, expired, foreign, or superseded enrollment) and `closed` both
 * end setup, since the proposed secret can no longer be proven; only
 * `invalid-code` (a wrong or replayed proof) keeps the same QR and secret
 * available for another attempt.
 */
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

/** Map a `DELETE .../totp` rejection. */
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

/** A QR module grid as one SVG path, one unit per module. */
export interface TotpQr {
  /** The path's square viewBox size (module grid width, quiet zone
   * included: `uqr`'s default one-module `border`). */
  readonly size: number;
  /** `M{x} {y}h1v1h-1z` for every dark module; empty modules add nothing. */
  readonly path: string;
}

/**
 * Encodes the provisioning URI into a QR module grid with the pinned `uqr`
 * encoder and reduces it to one SVG path string — no raw HTML, so the
 * component renders it with plain template bindings (`:d`, `:viewBox`)
 * instead of `v-html`. No network request; the path holds only numeric
 * module coordinates, never the URI as text
 * (docs/design/totp-second-factor-contract.md "Provisioning data").
 */
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
  /** Refresh the current session's recent-reauthentication time. */
  reauthenticate(password: string): Promise<void>;
  /** Begin the provider OAuth reauthentication round trip. */
  startProviderReauth(provider: AuthProvider): Promise<void>;
  /** Start TOTP enrollment or replacement; returns the one-time secret. */
  startEnrollment(): Promise<TotpEnrollmentStart>;
  /** Prove the proposed secret and install or replace the credential. */
  completeEnrollment(
    enrollmentId: string,
    code: string,
  ): Promise<TotpEnrollmentComplete>;
  /** Remove the active TOTP credential. */
  removeTotp(): Promise<void>;
}

interface PasswordReauthEnvelope {
  data?: { secondFactorRequired?: boolean };
}

/**
 * Every action here calls `useAuth().mutate` directly rather than through an
 * injected actions object, so `TotpSettings` needs no wiring from the
 * settings page: it owns its own network calls the same way
 * `ConnectedAgents`/`useAgentGrants` do.
 */
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
    // An account with another active factor answers with a pending row
    // instead of completing reauth outright; the pending second-factor page
    // returns here since this purpose's return path is fixed to settings.
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
