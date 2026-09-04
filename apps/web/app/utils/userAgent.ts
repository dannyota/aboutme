const MAX_USER_AGENT_LENGTH = 4096;

interface BrowserMatch {
  name: string;
  major: string;
}

function browserOf(ua: string): BrowserMatch | null {
  const match
    = ua.match(/Edg\/(\d+)/u)
      ?? ua.match(/HeadlessChrome\/(\d+)/u)
      ?? ua.match(/Chrome\/(\d+)/u)
      ?? ua.match(/Firefox\/(\d+)/u)
      ?? (ua.includes('Safari/') ? ua.match(/Version\/(\d+)/u) : null);
  if (!match?.[1]) return null;

  const name = ua.includes('Edg/')
    ? 'Edge'
    : ua.includes('HeadlessChrome/') || ua.includes('Chrome/')
      ? 'Chrome'
      : ua.includes('Firefox/')
        ? 'Firefox'
        : 'Safari';
  return { name, major: match[1] };
}

function operatingSystemOf(ua: string): string | null {
  if (ua.includes('Windows')) return 'Windows';
  if (ua.includes('Android')) return 'Android';
  if (ua.includes('iPhone') || ua.includes('iPad')) return 'iOS';
  if (ua.includes('Mac OS X')) return 'Mac OS X';
  if (ua.includes('Linux')) return 'Linux';
  return null;
}

export function describeUserAgent(ua: string): string {
  if (
    typeof ua !== 'string'
    || ua.length === 0
    || ua.length > MAX_USER_AGENT_LENGTH
    || Array.from(ua).some((character) => character.charCodeAt(0) > 0x7f)
  ) {
    return 'Unknown browser';
  }

  const browser = browserOf(ua);
  if (browser === null) return 'Unknown browser';
  const operatingSystem = operatingSystemOf(ua);
  return operatingSystem === null
    ? `${browser.name} ${browser.major}`
    : `${browser.name} ${browser.major} on ${operatingSystem}`;
}
