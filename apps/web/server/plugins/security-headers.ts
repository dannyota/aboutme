import {
  jsonLdScriptContent,
  scriptHashSource,
  withScriptSource,
} from '../utils/cspHash';

export default defineNitroPlugin((nitroApp) => {
  // @nuxt/nitro-server's renderer sets this on every rendered response
  // (renderer.mjs, both the buffered and streamed paths) with no config
  // knob to disable it. Strip it so no response names its server framework
  // (docs/design/security.md).
  nitroApp.hooks.hook('beforeResponse', (event) => {
    removeResponseHeader(event, 'x-powered-by');
  });

  // The homepage and template pages each render one inline JSON-LD script
  // (app/landing/structuredData.ts, app/templates/structuredData.ts). Every
  // other page's CSP (nuxt.config.ts routeRules) already forbids inline
  // scripts outright; augment it with that one script's own hash instead of
  // widening script-src for pages that have no inline script at all.
  nitroApp.hooks.hook('render:response', (response, { event }) => {
    if (typeof response.body !== 'string') return;
    const content = jsonLdScriptContent(response.body);
    if (content === null) return;
    const current = getResponseHeader(event, 'content-security-policy');
    if (typeof current !== 'string') return;
    setResponseHeader(
      event,
      'content-security-policy',
      withScriptSource(current, scriptHashSource(content)),
    );
  });
});
