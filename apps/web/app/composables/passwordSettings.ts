import type { InjectionKey } from 'vue';

import type { AuthProvider } from './useAuth';
import type { PasswordIssue } from './usePasswordAuth';

/**
 * `passwordSettings` — the closed contract behind the account-settings
 * password controls (add/change a password, password reauth, and provider
 * reauth).
 *
 * The `PasswordSettings` component is presentational: it receives the
 * current `hasPassword` status and the linked providers as props, and it
 * receives exactly three side-effecting actions through
 * `PasswordSettingsActionsKey`. Each action performs one network operation
 * (or the provider OAuth round trip) and rejects with a
 * `PasswordSettingsFailure` so the component can map to fixed copy without
 * ever reading a raw server body.
 */

export type PasswordSettingsErrorKind
  = | 'reauth-failed'
    | 'reauth-required'
    | 'password-invalid'
    | 'rate-limited'
    | 'unavailable';

/** Rejection value for every password-settings action. */
export class PasswordSettingsFailure extends Error {
  readonly kind: PasswordSettingsErrorKind;
  readonly issue?: PasswordIssue;

  constructor(kind: PasswordSettingsErrorKind, issue?: PasswordIssue) {
    super(`password settings failed: ${kind}`);
    this.name = 'PasswordSettingsFailure';
    this.kind = kind;
    if (issue !== undefined) this.issue = issue;
  }
}

export interface PasswordSettingsActions {
  /** Refresh the current session's recent-reauthentication time. */
  reauthenticate(password: string): Promise<void>;
  /** Add (provider-only) or replace (password) the account credential. */
  setPassword(password: string): Promise<void>;
  /** Begin the provider OAuth reauthentication round trip. */
  startProviderReauth(provider: AuthProvider): Promise<void>;
}

export const PasswordSettingsActionsKey: InjectionKey<PasswordSettingsActions>
  = Symbol('aboutme-password-settings-actions');

interface ErrorEnvelope {
  error?: {
    code?: unknown;
    details?: { issue?: unknown };
  };
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
  return (error as { data?: ErrorEnvelope } | null)?.data?.error?.code;
}

function failureIssue(error: unknown): unknown {
  return (error as { data?: ErrorEnvelope } | null)?.data?.error?.details
    ?.issue;
}

const ISSUES: readonly PasswordIssue[] = ['length', 'common', 'breached'];

function isIssue(value: unknown): value is PasswordIssue {
  return typeof value === 'string'
    && (ISSUES as readonly string[]).includes(value);
}

/**
 * Map a `POST /auth/password/reauth` rejection to the closed union. `401
 * reauth_failed` is a wrong current password; a lost session
 * (`authentication_required`) degrades to `unavailable`.
 */
export function mapReauthError(error: unknown): PasswordSettingsFailure {
  const status = failureStatus(error);
  const code = failureCode(error);
  if (status === 401 && code === 'reauth_failed') {
    return new PasswordSettingsFailure('reauth-failed');
  }
  if (status === 429 && code === 'rate_limited') {
    return new PasswordSettingsFailure('rate-limited');
  }
  return new PasswordSettingsFailure('unavailable');
}

/**
 * Map a `PUT /me/password` rejection to the closed union. `403
 * reauth_required` signals an expired/absent recent-reauthentication window;
 * `422 password_invalid` carries the closed policy issue. A surfaced
 * `csrf_rejected` (after `mutate`'s single self-heal retry) and a lost
 * session both degrade to `unavailable`.
 */
export function mapSetPasswordError(error: unknown): PasswordSettingsFailure {
  const status = failureStatus(error);
  const code = failureCode(error);
  const issue = failureIssue(error);
  if (status === 403 && code === 'reauth_required') {
    return new PasswordSettingsFailure('reauth-required');
  }
  if (status === 422 && code === 'password_invalid') {
    return new PasswordSettingsFailure(
      'password-invalid',
      isIssue(issue) ? issue : undefined,
    );
  }
  if (status === 429 && code === 'rate_limited') {
    return new PasswordSettingsFailure('rate-limited');
  }
  return new PasswordSettingsFailure('unavailable');
}

/**
 * Map a `POST /auth/{provider}/start?purpose=reauth` rejection to the closed
 * union. Only its own tighter rate limit is distinguished; everything else
 * is `unavailable` (the provider round trip never begins).
 */
export function mapReauthStartError(error: unknown): PasswordSettingsFailure {
  if (failureStatus(error) === 429 && failureCode(error) === 'rate_limited') {
    return new PasswordSettingsFailure('rate-limited');
  }
  return new PasswordSettingsFailure('unavailable');
}
