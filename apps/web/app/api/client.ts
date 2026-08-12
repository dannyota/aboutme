/**
 * Typed OpenAPI clients for the versioned product API and root health probes.
 * Each client has one base URL. Relative defaults are browser-only; a
 * server-side caller must supply an absolute base URL. Authenticated calls stay
 * in the browser because server-side rendering has no browser session cookie.
 */
import createClient, { type Client, type ClientOptions } from 'openapi-fetch';
import type { paths } from './generated/openapi';

/** Operations that OpenAPI pins to the unversioned site root. */
export const OPS_PATHS = ['/healthz', '/readyz'] as const;

/** Unversioned liveness/readiness surface, served from the site root. */
export type OpsPaths = Pick<paths, (typeof OPS_PATHS)[number]>;

/** Versioned product surface, served under `/api/v1`. */
export type ApiPaths = Omit<paths, (typeof OPS_PATHS)[number]>;

/** Default base path for the versioned API (same origin, see above). */
export const API_BASE_PATH = '/api/v1';

/** Default base path for the ops probes: the site root, i.e. no prefix. */
export const OPS_BASE_PATH = '';

/**
 * Anything `openapi-fetch` accepts — notably `baseUrl` (pass
 * `useRuntimeConfig().public.apiBase`, or an absolute URL server-side)
 * and `fetch` (inject a double in tests).
 */
export type ApiClientOptions = ClientOptions;

/**
 * A typed client for the versioned `/api/v1` surface.
 *
 * `credentials: 'include'` is stated rather than left to the browser
 * default because it is security-relevant: the session cookie is
 * `__Host-session`, httpOnly, and must travel with these calls for the
 * server to resolve a session at all.
 */
export function createApiClient(
  options: ApiClientOptions = {},
): Client<ApiPaths> {
  return createClient<ApiPaths>({
    baseUrl: API_BASE_PATH,
    credentials: 'include',
    ...options,
  });
}

/**
 * A typed client for the unversioned `/healthz` and `/readyz` probes.
 *
 * No credentials: the probes are anonymous (`security: []`) and must stay
 * answerable without a session.
 */
export function createOpsClient(
  options: ApiClientOptions = {},
): Client<OpsPaths> {
  return createClient<OpsPaths>({
    baseUrl: OPS_BASE_PATH,
    ...options,
  });
}
