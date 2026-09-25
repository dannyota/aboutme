import type { Ref } from 'vue';
import { watch } from 'vue';
import type { AuthState } from '@/composables/useAuth';
import { requiresSession, signedOutRedirect } from '@/utils/returnPath';

/**
 * Global route guard: every `/app/**` route and `/authorize` require a
 * signed-in session (docs/design/web.md, "Application surfaces" and "Agent
 * consent and connected agents"). `/app/**` is a client application whose
 * authenticated fetches never run in SSR, so this guard never blocks a
 * navigation on the session read: the shell has to appear at once. A read
 * already settled anonymous redirects at once. A read in flight (including
 * the re-read `useAuth` starts after an earlier rejection) is left to
 * `redirectWhenSettled`, which redirects only if it lands anonymous. A
 * session lost after the page settled signed in is left to the page
 * (`useResumeList.ts`, `pages/app/resumes/[id].vue`).
 */

interface Target {
  readonly path: string;
  readonly fullPath: string;
}

// The latest navigation this guard saw. The read can settle before that
// navigation commits, so the router's current route may still be the
// previous page; the latest target is where the visitor is going.
let latest: Target | null = null;

// At most one watcher runs at a time: a later navigation while one is
// pending must not stack a second redirect for the same settling read.
let watching = false;

function redirectWhenSettled(
  authState: Ref<AuthState>,
  nuxtApp: ReturnType<typeof useNuxtApp>,
): void {
  if (watching) return;
  watching = true;
  const stop = watch(authState, (state) => {
    if (state === 'loading') return;
    stop();
    watching = false;
    const target = latest;
    if (state !== 'anonymous' || target === null) return;
    // The visitor may have moved on to a public page while the read was in
    // flight.
    if (!requiresSession(target.path)) return;
    // A watcher callback runs outside the Nuxt context navigateTo needs.
    void nuxtApp.runWithContext(() =>
      navigateTo(signedOutRedirect(target.fullPath), { replace: true }));
  });
}

export default defineNuxtRouteMiddleware((to) => {
  if (import.meta.server) return;
  latest = { path: to.path, fullPath: to.fullPath };
  if (!requiresSession(to.path)) return;
  const { authState } = useAuth();
  if (authState.value === 'anonymous') {
    return navigateTo(signedOutRedirect(to.fullPath), { replace: true });
  }
  if (authState.value === 'loading') {
    redirectWhenSettled(authState, useNuxtApp());
  }
});
