import { describe, expect, it } from 'vitest';
import { describeUserAgent } from '../../app/utils/userAgent';

describe('describeUserAgent', () => {
  it.each([
    [
      'Chrome on Linux',
      'Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 '
      + '(KHTML, like Gecko) Chrome/152.0.0.0 Safari/537.36',
      'Chrome 152 on Linux',
    ],
    [
      'Headless Chrome on Linux',
      'Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 '
      + '(KHTML, like Gecko) HeadlessChrome/152.0.0.0 Safari/537.36',
      'Chrome 152 on Linux',
    ],
    [
      'Edge wins over Chrome',
      'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 '
      + '(KHTML, like Gecko) Chrome/152.0.0.0 Safari/537.36 Edg/152.0.0.0',
      'Edge 152 on Windows',
    ],
    [
      'Firefox on macOS',
      'Mozilla/5.0 (Macintosh; Intel Mac OS X 14.5; rv:128.0) '
      + 'Gecko/20100101 Firefox/128.0',
      'Firefox 128 on Mac OS X',
    ],
    [
      'Safari on iPhone',
      'Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) '
      + 'AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 '
      + 'Mobile/15E148 Safari/604.1',
      'Safari 17 on iOS',
    ],
    [
      'Chrome on Android',
      'Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 '
      + '(KHTML, like Gecko) Chrome/126.0.0.0 Mobile Safari/537.36',
      'Chrome 126 on Android',
    ],
  ])('%s', (_name, ua, expected) => {
    expect(describeUserAgent(ua)).toBe(expected);
  });

  it.each([
    ['', 'Unknown browser'],
    ['x'.repeat(4096), 'Unknown browser'],
    ['浏览器 🚀', 'Unknown browser'],
  ])('returns a safe fallback for hostile input', (ua, expected) => {
    expect(() => describeUserAgent(ua)).not.toThrow();
    expect(describeUserAgent(ua)).toBe(expected);
  });

  it('returns a safe fallback for non-string input', () => {
    expect(describeUserAgent(null as unknown as string)).toBe(
      'Unknown browser',
    );
    expect(describeUserAgent({} as unknown as string)).toBe('Unknown browser');
  });
});
