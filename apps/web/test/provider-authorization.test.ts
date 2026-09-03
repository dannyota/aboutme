import { afterEach, describe, expect, it } from 'vitest';

import { validateAuthorizeUrl } from '../app/composables/providerAuthorization';

describe('provider authorize URL validation', () => {
  afterEach(() => window.happyDOM.setURL('http://localhost:3000/'));

  it.each([
    ['google', 'https://accounts.google.com/o/oauth2/v2/auth?state=x'],
    ['github', 'https://github.com/login/oauth/authorize?state=x'],
    ['linkedin', 'https://www.linkedin.com/oauth/v2/authorization?state=x'],
  ] as const)('allows the exact %s provider endpoint', (provider, url) => {
    expect(validateAuthorizeUrl(provider, url)).toBe(url);
  });

  it('allows only the matching same-origin HTTPS loopback UAT endpoint', () => {
    window.happyDOM.setURL('https://localhost:20443/app/resumes/resume-1');
    const valid = 'https://localhost:20443/__uat/oauth/google/authorize?x=1';
    expect(validateAuthorizeUrl('google', valid)).toBe(valid);
    expect(
      validateAuthorizeUrl(
        'github',
        'https://localhost:20443/__uat/oauth/google/authorize',
      ),
    ).toBeNull();
    expect(
      validateAuthorizeUrl(
        'google',
        'https://localhost:20443/__uat/oauth/google/callback',
      ),
    ).toBeNull();
    expect(
      validateAuthorizeUrl(
        'google',
        'https://127.0.0.1:20443/__uat/oauth/google/authorize',
      ),
    ).toBeNull();
  });

  it('preserves the local HTTP authoring allowlist used by settings', () => {
    window.happyDOM.setURL('http://127.0.0.1:20080/app/settings/sessions');
    const url = 'http://127.0.0.1:20080/__uat/oauth/google/authorize/step';
    expect(validateAuthorizeUrl('google', url)).toBe(url);
  });

  it.each([
    'https://evil.example/authorize',
    'http://accounts.google.com/o/oauth2/v2/auth',
    'https://github.com/o/oauth2/v2/auth',
    'https://user:pass@accounts.google.com/o/oauth2/v2/auth',
    'https://accounts.google.com/o/oauth2/v2/auth#fragment',
    'not a URL',
  ])('rejects an untrusted authorize URL: %s', (url) => {
    expect(validateAuthorizeUrl('google', url)).toBeNull();
  });
});
