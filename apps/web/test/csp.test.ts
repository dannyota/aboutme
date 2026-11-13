// @vitest-environment node

import { describe, expect, it } from 'vitest';

import { APP_CSP, HTML_CSP } from '../app/utils/csp';

describe('HTML_CSP (public resume HTML and the harness)', () => {
  it('locks every directive down to no origin', () => {
    expect(HTML_CSP).toBe(
      'default-src \'none\'; base-uri \'none\'; object-src \'none\'; '
      + 'frame-ancestors \'none\'; form-action \'none\'; script-src \'self\'; '
      + 'style-src \'self\' \'unsafe-inline\'; img-src \'self\' data:; '
      + 'font-src \'self\'; connect-src \'self\'; manifest-src \'self\'; '
      + 'media-src \'none\'; worker-src \'none\'',
    );
  });

  it('keeps worker-src \'none\' for public HTML, print, and the harness '
    + '(docs/design/linkedin-import.md, ADR 0023 decision 4)', () => {
    expect(HTML_CSP).toContain('worker-src \'none\'');
  });
});

describe('APP_CSP (app pages: /, /login, /templates, /app/**)', () => {
  it('is as strict as the interactive app allows', () => {
    expect(APP_CSP).toBe(
      'default-src \'self\'; base-uri \'self\'; object-src \'none\'; '
      + 'frame-ancestors \'none\'; form-action \'self\'; script-src \'self\'; '
      + 'style-src \'self\' \'unsafe-inline\'; img-src \'self\' data:; '
      + 'font-src \'self\'; connect-src \'self\'; manifest-src \'self\'; '
      + 'media-src \'none\'; worker-src \'self\'',
    );
  });

  it('allows only a same-origin worker, per docs/design/linkedin-import.md '
    + 'and ADR 0023, with every other directive unchanged', () => {
    const workerSrc = APP_CSP.split(';')
      .map((part) => part.trim())
      .find((part) => part.startsWith('worker-src'));
    expect(workerSrc).toBe('worker-src \'self\'');

    const otherDirectives = (policy: string): string[] =>
      policy.split(';')
        .map((part) => part.trim())
        .filter((part) => !part.startsWith('worker-src'))
        .filter((part) =>
          !part.startsWith('default-src')
          && !part.startsWith('base-uri')
          && !part.startsWith('form-action'));
    expect(otherDirectives(APP_CSP)).toEqual(otherDirectives(HTML_CSP));
  });

  it('scopes to the app\'s own origin, unlike HTML_CSP\'s no origin', () => {
    expect(APP_CSP).toContain('default-src \'self\'');
    expect(APP_CSP).toContain('base-uri \'self\'');
    expect(HTML_CSP).toContain('default-src \'none\'');
    expect(HTML_CSP).toContain('base-uri \'none\'');
  });

  it('never allows inline or eval scripts', () => {
    expect(APP_CSP.split(';').find((part) => part.includes('script-src')))
      .toBe(' script-src \'self\'');
    expect(APP_CSP).toContain('style-src \'self\' \'unsafe-inline\'');
  });

  it('denies framing, embedding, and plugins', () => {
    expect(APP_CSP).toContain('frame-ancestors \'none\'');
    expect(APP_CSP).toContain('object-src \'none\'');
  });

  it('scopes forms and network calls to the app\'s own origin', () => {
    expect(APP_CSP).toContain('form-action \'self\'');
    expect(APP_CSP).toContain('connect-src \'self\'');
  });
});
