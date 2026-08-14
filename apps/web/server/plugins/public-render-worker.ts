import { defineNitroPlugin } from 'nitropack/runtime';

export default defineNitroPlugin(() => {
  // The generated worker URL is imported by the closed internal route. This
  // plugin intentionally has no network, storage, or application capability.
});
