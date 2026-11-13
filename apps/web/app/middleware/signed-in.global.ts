import type { Ref } from 'vue';
import { watch } from 'vue';
import type { Router } from 'vue-router';
import type { AuthState } from '@/composables/useAuth';
import { requiresSession, signedOutRedirect } from '@/utils/returnPath';

// Nuxt's navigateTo only returns a location while middleware is running.

interface Target {
  readonly path: string;
  readonly fullPath: string;
}

let latest: Target | null = null;

let watching = false;

function redirectWhenSettled(
  authState: Ref<AuthState>,
  router: Router,
): void {
  if (watching) return;
  watching = true;
  const stop = watch(authState, (state) => {
    if (state === 'loading') return;
    stop();
    watching = false;
    const target = latest;
    if (state !== 'anonymous' || target === null) return;
    if (!requiresSession(target.path)) return;
    void router.replace(signedOutRedirect(target.fullPath));
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
    redirectWhenSettled(authState, useRouter());
  }
});
