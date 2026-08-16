/**
 * `usePasswordAuth` — the password flows behind login, registration, email
 * verification, and password recovery.
 *
 * Each method wraps exactly one generated password operation. It maps only
 * exact status/code pairs from the API to a closed `PasswordAuthError` union
 * and falls back to `"unavailable"` for anything else, so a hostile or
 * drift-prone server body can never leak raw text or a prototype property
 * into the UI.
 *
 * It never stores a password or token in Nuxt state, route query,
 * local/session storage, an error object, a logger, or analytics. All
 * requests send `credentials: 'include'` so the `__Host-session` cookie (and
 * its exact same-origin pair) travel with the call.
 */

export type PasswordIssue = 'length' | 'common' | 'breached';

export type PasswordAuthError
  = | 'invalid-request'
    | 'invalid-token'
    | 'authentication-failed'
    | 'password-invalid'
    | 'rate-limited'
    | 'unavailable';

/** Rejection value for every password operation. */
export class PasswordAuthFailure extends Error {
  readonly kind: PasswordAuthError;
  readonly issue?: PasswordIssue;

  constructor(kind: PasswordAuthError, issue?: PasswordIssue) {
    super(`password auth failed: ${kind}`);
    this.name = 'PasswordAuthFailure';
    this.kind = kind;
    if (issue !== undefined) this.issue = issue;
  }
}

export interface UsePasswordAuth {
  register(input: {
    name: string;
    email: string;
    password: string;
  }): Promise<void>;
  verify(token: string): Promise<void>;
  login(input: { email: string; password: string }): Promise<void>;
  forgot(email: string): Promise<void>;
  reset(input: { token: string; password: string }): Promise<void>;
}

const ISSUES: readonly PasswordIssue[] = ['length', 'common', 'breached'];

function isIssue(value: unknown): value is PasswordIssue {
  return typeof value === 'string'
    && (ISSUES as readonly string[]).includes(value);
}

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

/**
 * Map an unknown thrown value to the closed failure union. Only the exact
 * status/code pairs declared by the password endpoints match; everything else
 * degrades to `unavailable`.
 */
export function mapPasswordAuthError(error: unknown): PasswordAuthFailure {
  const status = failureStatus(error);
  const code = failureCode(error);
  const issue = failureIssue(error);
  const exact = (
    wantStatus: number,
    wantCode: string,
    _kind: PasswordAuthError,
  ): boolean => status === wantStatus && code === wantCode;

  if (exact(400, 'request_invalid', 'invalid-request')) {
    return new PasswordAuthFailure('invalid-request');
  }
  if (exact(400, 'credential_token_invalid', 'invalid-token')) {
    return new PasswordAuthFailure('invalid-token');
  }
  if (exact(401, 'authentication_failed', 'authentication-failed')) {
    return new PasswordAuthFailure('authentication-failed');
  }
  if (exact(422, 'password_invalid', 'password-invalid')) {
    return new PasswordAuthFailure(
      'password-invalid',
      isIssue(issue) ? issue : undefined,
    );
  }
  if (exact(429, 'rate_limited', 'rate-limited')) {
    return new PasswordAuthFailure('rate-limited');
  }
  if (exact(503, 'authentication_unavailable', 'unavailable')) {
    return new PasswordAuthFailure('unavailable');
  }
  return new PasswordAuthFailure('unavailable');
}

async function post(
  url: string,
  body: Record<string, unknown>,
): Promise<void> {
  try {
    await $fetch<unknown>(url, {
      method: 'POST',
      body,
      credentials: 'include',
    });
  } catch (error) {
    throw mapPasswordAuthError(error);
  }
}

export function usePasswordAuth(): UsePasswordAuth {
  return {
    async register(input) {
      await post('/api/v1/auth/password/register', input);
    },
    async verify(token) {
      await post('/api/v1/auth/password/verify', { token });
    },
    async login(input) {
      await post('/api/v1/auth/password/login', input);
    },
    async forgot(email) {
      await post('/api/v1/auth/password/forgot', { email });
    },
    async reset(input) {
      await post('/api/v1/auth/password/reset', input);
    },
  };
}
