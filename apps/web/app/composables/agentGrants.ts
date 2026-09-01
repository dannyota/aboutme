import { ref, type Ref } from 'vue';
import { createApiClient } from '../api/client';
import type { components } from '../api/generated/openapi';
import { useAuth } from './useAuth';

export type AgentGrant = components['schemas']['OAuthAgentGrant'];
export type AgentGrantScope = components['schemas']['OAuthConsentScope'];
type GrantID = components['parameters']['GrantID'];

export type AgentGrantsError
  = 'session-required'
    | 'not-found'
    | 'unavailable';

export class AgentGrantsFailure extends Error {
  readonly kind: AgentGrantsError;

  constructor(kind: AgentGrantsError) {
    super(`connected agents failed: ${kind}`);
    this.name = 'AgentGrantsFailure';
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

/** Map only the exact session and stale-grant pairs to public failures. */
export function mapAgentGrantsError(error: unknown): AgentGrantsFailure {
  const status = statusOf(error);
  const code = codeOf(error);
  if (status === 401 && code === 'session_required') {
    return new AgentGrantsFailure('session-required');
  }
  if (status === 404 && code === 'not_found') {
    return new AgentGrantsFailure('not-found');
  }
  return new AgentGrantsFailure('unavailable');
}

const scopes: readonly AgentGrantScope[] = [
  'resumes:read',
  'resumes:write',
];

function isScope(value: unknown): value is AgentGrantScope {
  return typeof value === 'string'
    && (scopes as readonly string[]).includes(value);
}

function isIsoDate(value: unknown): value is string {
  return typeof value === 'string'
    && value !== ''
    && Number.isFinite(Date.parse(value));
}

function isGrant(value: unknown): value is AgentGrant {
  if (typeof value !== 'object' || value === null) return false;
  const candidate = value as {
    id?: unknown;
    clientName?: unknown;
    scopes?: unknown;
    createdAt?: unknown;
    lastUsedAt?: unknown;
  };
  return typeof candidate.id === 'string'
    && candidate.id !== ''
    && typeof candidate.clientName === 'string'
    && candidate.clientName !== ''
    && Array.isArray(candidate.scopes)
    && candidate.scopes.length > 0
    && candidate.scopes.length <= scopes.length
    && new Set(candidate.scopes).size === candidate.scopes.length
    && candidate.scopes.every(isScope)
    && isIsoDate(candidate.createdAt)
    && (candidate.lastUsedAt === null || isIsoDate(candidate.lastUsedAt));
}

function grantsFrom(value: unknown): AgentGrant[] | null {
  if (typeof value !== 'object' || value === null) return null;
  const data = (value as { data?: unknown }).data;
  if (typeof data !== 'object' || data === null) return null;
  const grants = (data as { grants?: unknown }).grants;
  if (!Array.isArray(grants) || !grants.every(isGrant)) return null;
  return grants;
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

export interface UseAgentGrants {
  grants: Readonly<Ref<readonly AgentGrant[]>>;
  refresh: () => Promise<void>;
  revoke: (grantId: GrantID) => Promise<void>;
}

export function useAgentGrants(): UseAgentGrants {
  const auth = useAuth();
  const grants = ref<AgentGrant[]>([]);

  async function refresh(): Promise<void> {
    try {
      // The registry bridge is test-only. Browser production calls retain the
      // generated client's normal same-origin credential behavior.
      const api = import.meta.env.MODE === 'test'
        ? createApiClient({
            baseUrl: 'http://localhost/api/v1',
            fetch: nuxtFetch,
          })
        : createApiClient();
      const { data, error, response } = await api.GET('/me/agents');
      const next = grantsFrom(data);
      if (next !== null) {
        grants.value = next;
        return;
      }
      throw mapAgentGrantsError({
        statusCode: response?.status,
        data: error,
      });
    } catch (error) {
      if (error instanceof AgentGrantsFailure) throw error;
      throw mapAgentGrantsError(error);
    }
  }

  async function revoke(grantId: GrantID): Promise<void> {
    try {
      const encodedId = encodeURIComponent(grantId);
      await auth.mutate(`/api/v1/me/agents/${encodedId}`, {
        method: 'DELETE',
      });
    } catch (error) {
      throw mapAgentGrantsError(error);
    }
  }

  return { grants, refresh, revoke };
}
