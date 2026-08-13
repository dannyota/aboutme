export const ALLOWED_ORIGIN = 'https://localhost:20443';

function parsedURL(value: string): URL | null {
  try {
    const url = new URL(value);
    if (url.username || url.password) return null;
    return url;
  } catch {
    return null;
  }
}

export function isAllowedHTTPURL(value: string): boolean {
  return parsedURL(value)?.origin === ALLOWED_ORIGIN;
}

export function isAllowedWebSocketURL(value: string): boolean {
  const url = parsedURL(value);
  return url?.protocol === 'wss:'
    && url.hostname === 'localhost'
    && url.port === '20443';
}
