/**
 * Renderer-surface baseline CSP, applied to `/_harness/**` in
 * nuxt.config.ts and asserted by apps/web/e2e/normal-csp.spec.ts and
 * corpus.spec.ts.
 */
export const HTML_CSP
  = 'default-src \'none\'; base-uri \'none\'; object-src \'none\'; '
    + 'frame-ancestors \'none\'; form-action \'none\'; script-src \'self\'; '
    + 'style-src \'self\' \'unsafe-inline\'; img-src \'self\' data:; '
    + 'font-src \'self\'; connect-src \'self\'; manifest-src \'self\'; '
    + 'media-src \'none\'; worker-src \'none\'';
