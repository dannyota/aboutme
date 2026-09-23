import { flushPromises } from '@vue/test-utils';

/**
 * Waits for the client router to reach `path`, then answers with where it
 * actually is, so a case that never arrives fails on the difference rather
 * than on a timeout. Resolving a route's component is real asynchronous work,
 * so this polls instead of flushing microtasks.
 */
export async function landedOn(
  path: string,
  timeoutMs = 2000,
): Promise<string> {
  const router = useRouter();
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline && router.currentRoute.value.fullPath !== path) {
    await new Promise((resolve) => setTimeout(resolve, 10));
    await flushPromises();
  }
  return router.currentRoute.value.fullPath;
}

/** Where the client router is now, for a case that must not navigate. */
export function currentPath(): string {
  return useRouter().currentRoute.value.fullPath;
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
