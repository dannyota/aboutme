// @vitest-environment node

import { describe, expect, it } from 'vitest';

import { APP_CSP, HTML_CSP } from '../app/utils/csp';

// Every fetch-directive keyword each policy must carry, and the ones it must
// never carry. A regression here is a real security-header gap, so pin exact
// strings rather than loose substring checks.

describe('HTML_CSP (renderer surfaces: public resume HTML and the harness)', () => {
  it('locks every directive down to no origin', () => {
    expect(HTML_CSP).toBe(
      'default-src \'none\'; base-uri \'none\'; object-src \'none\'; '
      + 'frame-ancestors \'none\'; form-action \'none\'; script-src \'self\'; '
      + 'style-src \'self\' \'unsafe-inline\'; img-src \'self\' data:; '
      + 'font-src \'self\'; connect-src \'self\'; manifest-src \'self\'; '
      + 'media-src \'none\'; worker-src \'none\'',
    );
  });
});

describe('APP_CSP (Nuxt-rendered app pages: /, /login, /templates, /app/**)', () => {
  it('is as strict as the interactive app allows', () => {
    expect(APP_CSP).toBe(
      'default-src \'self\'; base-uri \'self\'; object-src \'none\'; '
      + 'frame-ancestors \'none\'; form-action \'self\'; script-src \'self\'; '
      + 'style-src \'self\' \'unsafe-inline\'; img-src \'self\' data:; '
      + 'font-src \'self\'; connect-src \'self\'; manifest-src \'self\'; '
      + 'media-src \'none\'; worker-src \'none\'',
    );
  });

  it('never allows inline or eval scripts', () => {
    // The script-src directive carries only 'self': no 'unsafe-inline', no
    // 'unsafe-eval', and no nonce or hash baked into the static policy (a
    // page with an inline script, such as the homepage's JSON-LD, earns its
    // own response-specific hash source at render time instead; see
    // server/utils/cspHash.ts).
    expect(APP_CSP.split(';').find((part) => part.includes('script-src')))
      .toBe(' script-src \'self\'');
    // style-src is the one directive allowed to carry 'unsafe-inline': the
    // renderer's per-document customization writes CSS custom properties as
    // inline `style` attributes (app/components/resume/ResumeDocument.vue),
    // an unbounded, per-render value set no fixed hash list could cover.
    expect(APP_CSP).toContain('style-src \'self\' \'unsafe-inline\'');
  });

  it('denies framing and disables plugins', () => {
    expect(APP_CSP).toContain('frame-ancestors \'none\'');
    expect(APP_CSP).toContain('object-src \'none\'');
  });

  it('scopes navigation and network to the app\'s own origin', () => {
    expect(APP_CSP).toContain('default-src \'self\'');
    expect(APP_CSP).toContain('base-uri \'self\'');
    expect(APP_CSP).toContain('form-action \'self\'');
    expect(APP_CSP).toContain('connect-src \'self\'');
  });
});
