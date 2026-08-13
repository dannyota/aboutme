import { externalizeNuxtBootstrap } from '../utils/cspExternalize';

export default defineNitroPlugin((nitro) => {
  nitro.hooks.hook('render:response', (response) => {
    if (typeof response.body !== 'string') return;
    response.body = externalizeNuxtBootstrap(response.body);
  });
});
