export default defineNitroPlugin((nitroApp) => {
  // Nitro's renderer.mjs sets X-Powered-By with no option to disable it.
  nitroApp.hooks.hook('beforeResponse', (event) => {
    removeResponseHeader(event, 'x-powered-by');
  });
});
