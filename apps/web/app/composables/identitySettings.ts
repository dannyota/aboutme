import type { AuthProvider } from './useAuth';

/**
 * `identitySettings` — the closed contract behind the Unlink control for a
 * linked provider identity (`DELETE /me/identities/{identityId}`).
 *
 * `LinkedIdentities` is presentational: it receives the actions as a prop, and
 * each action rejects with an `UnlinkFailure` so the component maps a closed
 * kind to fixed copy without reading a raw server body.
 */

export type UnlinkFailureKind
  = | 'reauth-required'
    | 'reauth-failed'
    | 'not-found'
    | 'last-method'
    | 'rate-limited'
    | 'unavailable';

export class UnlinkFailure extends Error {
  readonly kind: UnlinkFailureKind;

  constructor(kind: UnlinkFailureKind) {
    super(`identity unlink failed: ${kind}`);
    this.name = 'UnlinkFailure';
    this.kind = kind;
  }
}

export interface LinkedIdentityActions {
  /** Unlink one identity by its id. */
  unlink(id: string): Promise<void>;
  /** Refresh the recent-reauthentication time with the current password. */
  reauthenticate(password: string): Promise<void>;
  /** Begin the provider OAuth reauthentication round trip. */
  startProviderReauth(provider: AuthProvider): Promise<void>;
}

interface ErrorEnvelope {
  error?: { code?: unknown };
}

function failureStatus(error: unknown): number | null {
  const candidate = error as { statusCode?: unknown; status?: unknown } | null;
  for (const key of ['statusCode', 'status'] as const) {
    const value = candidate?.[key];
    if (typeof value === 'number') return value;
  }
  return null;
}

/**
 * Map a `DELETE /me/identities/{identityId}` rejection. `403 reauth_required`
 * asks for a fresh reauthentication, `404` means the identity is already gone,
 * and `409 last_sign_in_method` refuses to remove the only sign-in method. A
 * surfaced `csrf_rejected` and every server error degrade to `unavailable`.
 */
export function mapUnlinkError(error: unknown): UnlinkFailure {
  const status = failureStatus(error);
  const code = (error as { data?: ErrorEnvelope } | null)?.data?.error?.code;
  if (status === 403 && code === 'reauth_required') {
    return new UnlinkFailure('reauth-required');
  }
  if (status === 404) return new UnlinkFailure('not-found');
  if (status === 409 && code === 'last_sign_in_method') {
    return new UnlinkFailure('last-method');
  }
  if (status === 429) return new UnlinkFailure('rate-limited');
  return new UnlinkFailure('unavailable');
}
