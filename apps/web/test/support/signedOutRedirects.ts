import { vi, type MockInstance } from 'vitest';
import type { Router } from 'vue-router';
import {
  requiresSession,
  signedOutRedirect,
} from '../../app/utils/returnPath';

type Replace = Router['replace'];

/**
 * Holds the redirect the shared route guard (middleware/signed-in.global.ts)
 * sends through `router.replace` when a mounted app route's session read
 * lands anonymous, so a test can assert it while the page stays on the route
 * the test chose. Only that redirect is held: the sign-in or registration
 * path whose `next` is the route last set through `replace` (as
 * `mountSuspended` sets it). Every other `replace` still navigates.
 */
export function holdSignedOutRedirects(): MockInstance<Replace> {
  const router = useRouter();
  if (vi.isMockFunction(router.replace)) router.replace.mockRestore();
  const replace = router.replace.bind(router);
  let mounted: string | null = null;
  return vi.spyOn(router, 'replace').mockImplementation((to) => {
    if (
      typeof to === 'string'
      && mounted !== null
      && requiresSession(mounted)
      && to === signedOutRedirect(mounted)
    ) {
      return Promise.resolve();
    }
    if (typeof to === 'string') mounted = to;
    return replace(to);
  });
}
