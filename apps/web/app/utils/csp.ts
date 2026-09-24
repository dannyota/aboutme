/**
 * Renderer-surface baseline CSP, applied to `/_harness/**` in
 * nuxt.config.ts and asserted by apps/web/e2e/normal-csp.spec.ts and
 * corpus.spec.ts. Fully non-interactive: no base tag, no form, no origin of
 * its own, matching apps/server/internal/publicformat.BaseCSP for public
 * resume HTML.
 */
export const HTML_CSP
  = 'default-src \'none\'; base-uri \'none\'; object-src \'none\'; '
    + 'frame-ancestors \'none\'; form-action \'none\'; script-src \'self\'; '
    + 'style-src \'self\' \'unsafe-inline\'; img-src \'self\' data:; '
    + 'font-src \'self\'; connect-src \'self\'; manifest-src \'self\'; '
    + 'media-src \'none\'; worker-src \'none\'';

/**
 * Baseline CSP for Nuxt-rendered app pages (`/`, `/login`, `/templates/**`,
 * `/app/**`, and every other route `nuxt.config.ts` does not give a more
 * specific policy): as strict as the interactive app allows rather than the
 * fully locked-down `HTML_CSP`. `default-src`, `base-uri`, and `form-action`
 * scope to the app's own origin instead of `'none'` because the app itself
 * is a real origin with same-origin forms and no `<base>` tag to forbid.
 *
 * `script-src 'self'` never carries `'unsafe-inline'` or `'unsafe-eval'`:
 * Nuxt's own hydration payload is already externalized into a same-origin
 * script file for every response (server/utils/cspExternalize.ts), and a
 * page with its own inline script (the homepage and template pages' JSON-LD)
 * earns a response-specific `'sha256-<hash>'` source at render time instead
 * (server/utils/cspHash.ts, server/plugins/security-headers.ts), the same
 * mechanism public resume HTML uses (apps/server/internal/publicformat/
 * jsonld.go). The editor's document validator no longer needs an exception
 * either: it used to call `ajv.compile()` in the browser, which emits a
 * `new Function`, so it is now a build-time Ajv standalone compile committed
 * as a plain module instead (app/editor/documentValidator.generated.mjs,
 * scripts/generate-document-validator.mjs).
 *
 * `style-src` keeps `'unsafe-inline'`: the resume renderer writes
 * per-document customization as inline `style` attributes
 * (app/components/resume/ResumeDocument.vue's `:style="rootStyle"` and
 * `:style="model.styles.header"`), an unbounded, per-render value set no
 * fixed hash or nonce list could cover.
 *
 * `img-src` allows `data:` for the photo crop preview, which reads the
 * locally selected file as a data URL before upload
 * (app/editor/photoController.ts); no page uses a `blob:` image, so it is
 * not listed. Every download (PDF, the privacy data export, TOTP recovery
 * codes) revokes a `blob:` object URL through a synthetic anchor click,
 * which CSP's fetch directives do not govern.
 *
 * `connect-src 'self'` covers every fetch, EventSource, and WebAuthn call
 * the app makes; none of it crosses origins. Google sign-in never loads a
 * Google script or calls a Google endpoint from this origin: it is a plain
 * `<a href>` top-level navigation to this app's own `/api/v1/auth/google/
 * start`, which redirects the browser to Google server-side
 * (app/components/auth/ProviderButtons.vue), so `connect-src` and
 * `script-src` never need to name Google at all.
 */
export const APP_CSP
  = 'default-src \'self\'; base-uri \'self\'; object-src \'none\'; '
    + 'frame-ancestors \'none\'; form-action \'self\'; script-src \'self\'; '
    + 'style-src \'self\' \'unsafe-inline\'; img-src \'self\' data:; '
    + 'font-src \'self\'; connect-src \'self\'; manifest-src \'self\'; '
    + 'media-src \'none\'; worker-src \'none\'';
