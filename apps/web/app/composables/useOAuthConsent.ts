/**
 * Session-authenticated agent-consent operations.
 *
 * The generated OpenAPI types are the only source for request and response
 * shapes. Errors are deliberately reduced to a closed union before a page
 * can render them.
 */
import { createApiClient } from '../api/client';
import type { components } from '../api/generated/openapi';
import { useAuth } from './useAuth';

export type OAuthConsentRequest = Omit<
  components['schemas']['OAuthConsentDecisionRequest'],
  'decision'
>;
export type OAuthConsentDecision
  = components['schemas']['OAuthConsentDecisionRequest']['decision'];
export type OAuthConsentScope = components['schemas']['OAuthConsentScope'];
export type OAuthConsentView
  = components['schemas']['OAuthConsentResponse']['data'];
export type OAuthConsentDecisionResult
  = components['schemas']['OAuthConsentDecisionResponse']['data'];

export type OAuthConsentError
  = 'session-required' | 'invalid-request' | 'unavailable';

export class OAuthConsentFailure extends Error {
  readonly kind: OAuthConsentError;

  constructor(kind: OAuthConsentError) {
    super(`oauth consent failed: ${kind}`);
    this.name = 'OAuthConsentFailure';
    this.kind = kind;
  }
}

function statusOf(error: unknown): number | null {
  if (typeof error !== 'object' || error === null) return null;
  const value = error as { statusCode?: unknown; status?: unknown };
  if (typeof value.statusCode === 'number') return value.statusCode;
  if (typeof value.status === 'number') return value.status;
  return null;
}

function codeOf(error: unknown): unknown {
  if (typeof error !== 'object' || error === null) return undefined;
  const value = error as {
    data?: { error?: { code?: unknown } };
    error?: { code?: unknown };
  };
  return value.data?.error?.code ?? value.error?.code;
}

/** Map only exact status/code pairs to the closed consent failure union. */
export function mapOAuthConsentError(error: unknown): OAuthConsentFailure {
  const status = statusOf(error);
  const code = codeOf(error);
  if (status === 401 && code === 'session_required') {
    return new OAuthConsentFailure('session-required');
  }
  if (
    (status === 400 && code === 'request_invalid')
    || (status === 404 && code === 'not_found')
  ) {
    return new OAuthConsentFailure('invalid-request');
  }
  return new OAuthConsentFailure('unavailable');
}

const SCOPES: readonly OAuthConsentScope[] = [
  'resumes:read',
  'resumes:write',
];

function isScope(value: unknown): value is OAuthConsentScope {
  return typeof value === 'string'
    && (SCOPES as readonly string[]).includes(value);
}

function isConsentView(value: unknown): value is OAuthConsentView {
  if (typeof value !== 'object' || value === null) return false;
  const candidate = value as { clientName?: unknown; scopes?: unknown };
  return typeof candidate.clientName === 'string'
    && candidate.clientName !== ''
    && Array.isArray(candidate.scopes)
    && candidate.scopes.length > 0
    && candidate.scopes.length <= SCOPES.length
    && candidate.scopes.every(isScope);
}

function isDecisionResult(
  value: unknown,
): value is OAuthConsentDecisionResult {
  if (typeof value !== 'object' || value === null) return false;
  const redirectTo = (value as { redirectTo?: unknown }).redirectTo;
  return typeof redirectTo === 'string' && redirectTo !== '';
}

async function nuxtFetch(
  request: Request,
  init?: RequestInit,
): Promise<Response> {
  const parsed = new URL(request.url);
  const target = parsed.hostname === 'localhost'
    ? `${parsed.pathname}${parsed.search}`
    : request.url;
  const result = await $fetch.raw<string>(target, {
    ...init,
    method: init?.method as
      | 'GET' | 'POST' | 'PUT' | 'DELETE' | 'PATCH' | 'HEAD' | undefined,
    ignoreResponseError: true,
    responseType: 'text',
  });
  const body = typeof result._data === 'string'
    ? result._data
    : JSON.stringify(result._data ?? null);
  return new Response(body, {
    status: result.status,
    statusText: result.statusText,
    headers: result.headers,
  });
}

export interface UseOAuthConsent {
  get(query: OAuthConsentRequest): Promise<OAuthConsentView>;
  decide(
    query: OAuthConsentRequest,
    decision: OAuthConsentDecision,
  ): Promise<OAuthConsentDecisionResult>;
}

export function useOAuthConsent(): UseOAuthConsent {
  const auth = useAuth();

  async function get(query: OAuthConsentRequest): Promise<OAuthConsentView> {
    // Build the client at call time so this authenticated operation cannot run
    // during SSR. The absolute same-origin base also supports Nuxt's Node
    // backed test runtime, whose Request constructor rejects relative URLs.
    // The bridge exists only for the Nuxt endpoint registry. Production uses
    // the shared client's normal browser fetch and credential semantics.
    const api = import.meta.env.MODE === 'test'
      ? createApiClient({
          baseUrl: 'http://localhost/api/v1',
          fetch: nuxtFetch,
        })
      : createApiClient();
    const { data, error, response } = await api.GET('/oauth/consent', {
      params: { query },
    });
    if (data !== undefined && isConsentView(data.data)) return data.data;
    throw mapOAuthConsentError({
      statusCode: response?.status,
      data: error,
    });
  }

  async function decide(
    query: OAuthConsentRequest,
    decision: OAuthConsentDecision,
  ): Promise<OAuthConsentDecisionResult> {
    try {
      const body: components['schemas']['OAuthConsentDecisionRequest'] = {
        ...query,
        decision,
      };
      const result = await auth.mutate<unknown>('/api/v1/oauth/consent', {
        method: 'POST',
        body: body as unknown as Record<string, unknown>,
      });
      if (
        typeof result === 'object'
        && result !== null
        && isDecisionResult((result as { data?: unknown }).data)
      ) {
        return (result as { data: OAuthConsentDecisionResult }).data;
      }
      throw new OAuthConsentFailure('unavailable');
    } catch (error) {
      if (error instanceof OAuthConsentFailure) throw error;
      throw mapOAuthConsentError(error);
    }
  }

  return { get, decide };
}
