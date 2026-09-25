import type { Ref } from 'vue';
import { watch } from 'vue';
import type { AuthState } from '@/composables/useAuth';
import { requiresSession, signedOutRedirect } from '@/utils/returnPath';

/**
 * Global route guard: every `/app/**` route and `/authorize` require a
 * signed-in session (docs/design/web.md, "Application surfaces" and "Agent
 * consent and connected agents"). `/app/**` is a client application whose
 * authenticated fetches never run in SSR, so this guard must not block the
 * initial render while the session read is in flight: the shell has to
 * appear at once. An anonymous state that has already settled redirects
 * immediately; a `'loading'` state is left to `redirectWhenSettled`, which
 * waits for the read to leave `'loading'` and redirects only if it lands
 * anonymous. This runs once per page load. A session lost later, after the
 * page has already rendered authenticated, is left to the page
 * (`useResumeList.ts`, `pages/app/resumes/[id].vue`).
 */

// At most one watcher runs at a time: a later navigation while one is
// pending must not stack a second redirect for the same settling read.
let watching = false;

function redirectWhenSettled(
  authState: Ref<AuthState>,
  router: ReturnType<typeof useRouter>,
  nuxtApp: ReturnType<typeof useNuxtApp>,
): void {
  if (watching) return;
  watching = true;
  const stop = watch(authState, (state) => {
    if (state === 'loading') return;
    stop();
    watching = false;
    if (state !== 'anonymous') return;
    // The visitor may have navigated away from the route that triggered
    // this watcher while the read was in flight, so redirect based on the
    // router's current route rather than the one the middleware captured.
    const current = router.currentRoute.value;
    if (!requiresSession(current.path)) return;
    // A watcher callback runs outside the Nuxt context that navigateTo
    // needs.
    void nuxtApp.runWithContext(() =>
      navigateTo(signedOutRedirect(current.fullPath), { replace: true }));
  });
}

export default defineNuxtRouteMiddleware((to) => {
  if (import.meta.server || !requiresSession(to.path)) return;
  const { authState } = useAuth();
  if (authState.value === 'anonymous') {
    return navigateTo(signedOutRedirect(to.fullPath), { replace: true });
  }
  if (authState.value === 'loading') {
    redirectWhenSettled(authState, useRouter(), useNuxtApp());
  }
});
