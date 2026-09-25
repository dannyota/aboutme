// @vitest-environment node

import { describe, expect, it } from 'vitest';

import {
  requiresSession,
  signedOutRedirect,
  validateReturnPath,
} from '../app/utils/returnPath';

describe('login and registration return path', () => {
  it.each([
    ['/app/resumes', '/app/resumes'],
    [
      '/app/resumes/0190f0e2-0000-7000-8000-000000000001',
      '/app/resumes/0190f0e2-0000-7000-8000-000000000001',
    ],
    [
      '/authorize?client_id=c&redirect_uri=https%3A%2F%2Fclient.example%2Fcb'
      + '&state=a%5Cb',
      '/authorize?client_id=c&redirect_uri=https%3A%2F%2Fclient.example%2Fcb'
      + '&state=a%5Cb',
    ],
    ['/' + 'a'.repeat(2047), '/' + 'a'.repeat(2047)],
    ['/app/resumes/a..b/.well', '/app/resumes/a..b/.well'],
    ['/authorize?path=/../x', '/authorize?path=/../x'],
  ])('keeps the same-origin path %s', (value, want) => {
    expect(validateReturnPath(value)).toBe(want);
  });

  it.each([
    undefined,
    null,
    ['/app/resumes'],
    '',
    'app/resumes',
    '//evil.example',
    'https://evil.example',
    'javascript:alert(1)',
    '/\\evil.example',
    '/%2F%2Fevil.example',
    '/%2Fevil.example',
    '/%5Cevil.example',
    '/app/%5C%5Cevil',
    '/\t/evil.example',
    '/app\n/x',
    '/app\r/x',
    '/app\x00',
    '/app\x7f',
    '/%09/evil.example',
    '/app%0A',
    '/app/%zz',
    '/./x',
    '/../x',
    '/.//evil.example',
    '/%2e//evil.example',
    '/app/new/..',
    '/a/%2E%2E/b',
    '/' + 'a'.repeat(2048),
  ])('rejects %j', (value) => {
    expect(validateReturnPath(value)).toBeNull();
  });

  it.each([
    ['/app/new', '/app/new'],
    ['/app/new?sample=ats-plain&lng=vi', '/app/new?sample=ats-plain&lng=vi'],
    [
      '/app/new?template=engineer-compact',
      '/app/new?template=engineer-compact',
    ],
    [
      '/app/new?sample=ats-plain&next=//evil.example',
      '/app/new?sample=ats-plain',
    ],
    ['/app/new?sample=ats-plain&sample=minimal-air&lng=en', '/app/new?lng=en'],
    ['/app/new?sample=unknown&template=../x', '/app/new'],
    ['/app/new?lng=en-US', '/app/new'],
    ['/app/new?lng=VI&template=ats-plain', '/app/new?template=ats-plain'],
    ['/app/new?lng=en#fragment', '/app/new?lng=en'],
    ['/APP/New/?sample=ats-plain&x=1', '/app/new?sample=ats-plain'],
    ['/app/%6Eew?x=1', '/app/new'],
  ])('keeps only the checked query of %s', (value, want) => {
    expect(validateReturnPath(value)).toBe(want);
  });
});

describe('requiresSession', () => {
  it.each([
    '/app/resumes',
    '/app/new?sample=x',
    '/app/settings/sessions',
    '/APP/Settings/Sessions/',
    '/authorize?client_id=a',
    '/Authorize/',
  ])('is true for %s', (path) => {
    expect(requiresSession(path)).toBe(true);
  });

  it.each([
    '/',
    '/app',
    '/apps',
    '/application/x',
    '/templates',
    '/login',
    '/login?next=/app/resumes',
  ])('is false for %s', (path) => {
    expect(requiresSession(path)).toBe(false);
  });
});

describe('signedOutRedirect', () => {
  it('sends /app/settings/sessions to login with next', () => {
    expect(signedOutRedirect('/app/settings/sessions')).toBe(
      '/login?next=%2Fapp%2Fsettings%2Fsessions',
    );
  });

  it('sends /app/new to register, dropping unchecked query values', () => {
    const next = encodeURIComponent(
      '/app/new?sample=engineer-compact&lng=en',
    );
    expect(
      signedOutRedirect(
        '/app/new?sample=engineer-compact&lng=en&x=1',
      ),
    ).toBe(`/register?next=${next}`);
  });

  it('keeps the full query of /authorize in next', () => {
    const next = encodeURIComponent('/authorize?client_id=a&state=b');
    expect(signedOutRedirect('/authorize?client_id=a&state=b')).toBe(
      `/login?next=${next}`,
    );
  });

  it('drops next for a path over the byte limit', () => {
    expect(signedOutRedirect('/' + 'a'.repeat(2048))).toBe('/login');
  });
});
