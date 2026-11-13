import createClient, { type Client, type ClientOptions } from 'openapi-fetch';
import type { paths } from './generated/openapi';

export type ApiPaths = Omit<paths, '/healthz' | '/readyz'>;

export const API_BASE_PATH = '/api/v1';

export type ApiClientOptions = ClientOptions;

export function createApiClient(
  options: ApiClientOptions = {},
): Client<ApiPaths> {
  return createClient<ApiPaths>({
    baseUrl: API_BASE_PATH,
    credentials: 'include',
    ...options,
  });
}
