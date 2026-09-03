import type { AuthProvider } from './useAuth';

const providerAuthorizeEndpoints: Record<AuthProvider, string> = {
  google: 'https://accounts.google.com/o/oauth2/v2/auth',
  github: 'https://github.com/login/oauth/authorize',
  linkedin: 'https://www.linkedin.com/oauth/v2/authorization',
};

function isLoopbackHostname(hostname: string): boolean {
  return (
    hostname === 'localhost' || hostname === '127.0.0.1' || hostname === '[::1]'
  );
}

/** Validate the only authorize URLs that privileged browser flows may open. */
export function validateAuthorizeUrl(
  provider: AuthProvider,
  value: unknown,
): string | null {
  if (typeof value !== 'string') return null;
  let candidate: URL;
  try {
    candidate = new URL(value);
  } catch {
    return null;
  }
  if (candidate.username || candidate.password || candidate.hash) return null;
  if (candidate.protocol === 'https:') {
    const expected = new URL(providerAuthorizeEndpoints[provider]);
    if (
      candidate.origin === expected.origin
      && candidate.pathname === expected.pathname
    ) {
      return candidate.href;
    }

    if (typeof window === 'undefined') return null;
    const current = new URL(window.location.href);
    const localUAT
      = current.protocol === 'https:'
        && isLoopbackHostname(current.hostname)
        && candidate.origin === current.origin
        && candidate.pathname === `/__uat/oauth/${provider}/authorize`;
    return localUAT ? candidate.href : null;
  }
  if (typeof window === 'undefined') return null;
  const current = new URL(window.location.href);
  const localUAT
    = candidate.protocol === 'http:'
      && current.protocol === 'http:'
      && isLoopbackHostname(current.hostname)
      && candidate.origin === current.origin
      && candidate.pathname.startsWith('/__uat/oauth/');
  return localUAT ? candidate.href : null;
}
