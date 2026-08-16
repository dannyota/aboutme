export const ALLOWED_ORIGIN = 'https://localhost:20443';
export const DIRECT_RENDER_ORIGIN = 'http://127.0.0.1:20440';

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

export function isExpectedNegativeHTTPConsole(
  message: string,
  value: string,
): boolean {
  const url = parsedURL(value);
  if (url?.origin !== ALLOWED_ORIGIN) return false;
  if (
    message === 'Failed to load resource: the server responded with a status of 403 ()'
  ) {
    return url.pathname === '/api/v1/auth/google/start'
      && url.search === '?purpose=reauth';
  }
  if (
    message === 'Failed to load resource: the server responded with a status of 401 ()'
  ) {
    return (url.pathname === '/api/v1/me' && url.search === '')
      || (url.pathname === '/api/v1/auth/password/login' && url.search === '');
  }
  if (
    message === 'Failed to load resource: the server responded with a status of 400 ()'
  ) {
    return url.pathname === '/api/v1/auth/password/reset' && url.search === '';
  }
  return false;
}

export function httpFailureStatus(message: string): number | null {
  const match = message.match(
    /^Failed to load resource: the server responded with a status of ([1-5][0-9]{2}) \([^\r\n]*\)$/,
  );
  return match?.[1] === undefined ? null : Number(match[1]);
}
