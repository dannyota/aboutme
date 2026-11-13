import { vi, type MockInstance } from 'vitest';
import type { Router } from 'vue-router';
import {
  requiresSession,
  signedOutRedirect,
} from '../../app/utils/returnPath';

type Replace = Router['replace'];

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
