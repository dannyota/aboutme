/**
 * Typed OpenAPI client for the versioned product API. Its relative default
 * base URL is browser-only; a server-side caller must supply an absolute base
 * URL. Authenticated calls stay in the browser because server-side rendering
 * has no browser session cookie.
 */
import createClient, { type Client, type ClientOptions } from 'openapi-fetch';
import type { paths } from './generated/openapi';

/**
 * Versioned product surface, served under `/api/v1`. OpenAPI pins the
 * `/healthz` and `/readyz` probes to the unversioned site root, so they are
 * left out.
 */
export type ApiPaths = Omit<paths, '/healthz' | '/readyz'>;

/** Default base path for the versioned API (same origin, see above). */
export const API_BASE_PATH = '/api/v1';

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
