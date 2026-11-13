import type { InjectionKey } from 'vue';

import type { AuthProvider } from './useAuth';
import type { PasswordIssue } from './usePasswordAuth';

export type PasswordSettingsErrorKind
  = | 'reauth-failed'
    | 'reauth-required'
    | 'password-invalid'
    | 'rate-limited'
    | 'unavailable';

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
  reauthenticate(password: string): Promise<void>;
  setPassword(password: string): Promise<void>;
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

export function mapReauthStartError(error: unknown): PasswordSettingsFailure {
  if (failureStatus(error) === 429 && failureCode(error) === 'rate_limited') {
    return new PasswordSettingsFailure('rate-limited');
  }
  return new PasswordSettingsFailure('unavailable');
}
