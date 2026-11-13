import type { AuthProvider } from './useAuth';

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
  unlink(id: string): Promise<void>;
  reauthenticate(password: string): Promise<void>;
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
