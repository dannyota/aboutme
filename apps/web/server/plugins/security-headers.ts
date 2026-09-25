export default defineNitroPlugin((nitroApp) => {
  // @nuxt/nitro-server's renderer sets this on every rendered response
  // (renderer.mjs, both the buffered and streamed paths) with no config
  // knob to disable it. Strip it so no response names its server framework
  // (docs/design/security.md).
  nitroApp.hooks.hook('beforeResponse', (event) => {
    removeResponseHeader(event, 'x-powered-by');
  });
});
