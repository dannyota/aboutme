/**
 * client.ts — the web app's typed HTTP client.
 *
 * `./generated/openapi.ts` is produced from `docs/api/openapi.yaml` by a
 * pinned `openapi-typescript` (`make api-gen`) and is drift-checked
 * against the contract by `make api-check`. This module is the only
 * hand-written part: it turns those path/schema types into a real client
 * with `openapi-fetch`, so a request URL, method, or response shape that
 * the contract does not describe cannot compile.
 *
 * ## Two clients, because the contract has two servers
 *
 * `openapi.yaml` serves the product API from `/api/v1`, but overrides
 * `/healthz` and `/readyz` back to the bare site root — health is
 * infrastructure, not product API, so a future `/api/v2` must never break
 * orchestrator or synthetic checks (design doc §2 route table).
 *
 * One `openapi-fetch` client has exactly one `baseUrl`, so a single
 * client over all of `paths` would silently send `/api/v1/healthz`, which
 * nothing serves. Splitting the path map instead makes that a compile
 * error: the versioned client does not accept `/healthz`, and the ops
 * client does not accept a versioned path.
 *
 * ## Base paths are relative, and therefore browser-only
 *
 * Dev and prod are one origin (Caddy routes `/api/v1/*` to the Go API),
 * so the defaults below are paths, not absolute URLs. `Request` resolves
 * a relative URL against the document — which exists in the browser and
 * does not exist in Nitro during SSR. Server-side callers must pass an
 * absolute `baseUrl`. Authenticated calls must not run during SSR at all
 * (see `useAuth`'s DD-C9 note): there is no browser cookie to send.
 */
import createClient, { type Client, type ClientOptions } from 'openapi-fetch';
import type { paths } from './generated/openapi';

/**
 * The operations `openapi.yaml` pins to the unversioned site root via a
 * path-level `servers` override.
 */
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
