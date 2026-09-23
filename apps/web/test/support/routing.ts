import { flushPromises } from '@vue/test-utils';

/**
 * The client router's full path once it stops moving away from the page at
 * `from`, which is a path without its query. Resolving a route's component is
 * real asynchronous work, so this polls instead of flushing microtasks, and it
 * answers as soon as the page changes.
 */
export async function landedFrom(
  from: string,
  timeoutMs = 2000,
): Promise<string> {
  const router = useRouter();
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline && router.currentRoute.value.path === from) {
    await new Promise((resolve) => setTimeout(resolve, 10));
    await flushPromises();
  }
  return router.currentRoute.value.fullPath;
}

/**
 * The state Nuxt holds for the whole of every client-side navigation, from
 * the first guard until the router settles. `navigateTo` refuses to move the
 * router while it is set.
 */
export function markNavigationInFlight(): void {
  useNuxtApp()._processingMiddleware = true;
}

/** Clears it, so one case's navigation cannot leak into the next. */
export function clearNavigationInFlight(): void {
  delete useNuxtApp()._processingMiddleware;
}
