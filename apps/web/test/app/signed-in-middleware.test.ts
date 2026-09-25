import { beforeEach, describe, expect, it, vi } from 'vitest';
import { mockNuxtImport, registerEndpoint } from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { setResponseStatus } from 'h3';
import type { RouteLocationNormalized } from 'vue-router';
import signedInMiddleware from '../../app/middleware/signed-in.global';

const me = {
  data: {
    user: {
      id: 'user-1',
      email: 'dev@aboutme.invalid',
      name: 'Dev User',
      avatarKey: null,
      hasPassword: true,
    },
    csrfToken: 'csrf',
    identities: [],
  },
};
let meStatus = 401;
// Holds every /me response until released, so a test can observe the
// guard while the session read is still loading.
let meGate: Promise<void> = Promise.resolve();
let releaseMe: () => void = () => {};
function holdMe(): void {
  meGate = new Promise((resolve) => {
    releaseMe = resolve;
  });
}
mockNuxtImport('navigateTo', () => vi.fn());
registerEndpoint('/api/v1/me', async (event) => {
  await meGate;
  if (meStatus !== 200) {
    setResponseStatus(event, meStatus);
    return { error: { code: 'session_required', message: 'Sign in.' } };
  }
  return me;
});

function route(
  path: string,
  fullPath = path,
): RouteLocationNormalized {
  return { path, fullPath } as unknown as RouteLocationNormalized;
}

const SESSIONS_LOGIN = '/login?next=%2Fapp%2Fsettings%2Fsessions';

// The shared signed-out route guard (docs/design/web.md, Application
// surfaces). A router push to an app route runs the registered global guard
// too; both calls share one pending watcher, so each case counts redirects.
describe('signed-in.global middleware', () => {
  beforeEach(async () => {
    meStatus = 401;
    meGate = Promise.resolve();
    await useRouter().push('/');
    clearNuxtData();
    vi.mocked(navigateTo).mockClear();
  });

  it('does nothing for a path that requires no session', async () => {
    const result = signedInMiddleware(
      route('/templates'),
      route('/templates'),
    );
    expect(result).toBeUndefined();
    await flushPromises();
    expect(vi.mocked(navigateTo)).not.toHaveBeenCalled();
  });

  it('redirects at once when the session is already settled anonymous',
    async () => {
      const auth = useAuth();
      await auth.refresh();
      expect(auth.authState.value).toBe('anonymous');

      signedInMiddleware(
        route('/app/settings/sessions'),
        route('/app/settings/sessions'),
      );

      expect(vi.mocked(navigateTo)).toHaveBeenCalledWith(
        SESSIONS_LOGIN,
        { replace: true },
      );
    });

  it('waits without blocking, then redirects once settled anonymous',
    async () => {
      holdMe();
      await useRouter().push('/app/settings/sessions');
      const auth = useAuth();
      expect(auth.authState.value).toBe('loading');

      const result = signedInMiddleware(
        route('/app/settings/sessions'),
        route('/'),
      );
      expect(result).toBeUndefined();
      expect(vi.mocked(navigateTo)).not.toHaveBeenCalled();

      releaseMe();
      await vi.waitFor(() => {
        expect(vi.mocked(navigateTo)).toHaveBeenCalledWith(
          SESSIONS_LOGIN,
          { replace: true },
        );
      });
      await flushPromises();
      expect(vi.mocked(navigateTo)).toHaveBeenCalledTimes(1);
    });

  it('does not redirect when the settling read lands authenticated',
    async () => {
      meStatus = 200;
      holdMe();
      await useRouter().push('/app/settings/sessions');
      const auth = useAuth();
      signedInMiddleware(route('/app/settings/sessions'), route('/'));

      releaseMe();
      await vi.waitFor(() => {
        expect(auth.authState.value).toBe('authenticated');
      });
      await flushPromises();
      expect(vi.mocked(navigateTo)).not.toHaveBeenCalled();
    });

  it('skips the redirect when the current route no longer needs a session',
    async () => {
      holdMe();
      const router = useRouter();
      await router.push('/app/settings/sessions');
      const auth = useAuth();
      signedInMiddleware(route('/app/settings/sessions'), route('/'));
      await router.push('/templates');

      releaseMe();
      await vi.waitFor(() => {
        expect(auth.authState.value).toBe('anonymous');
      });
      await flushPromises();
      expect(vi.mocked(navigateTo)).not.toHaveBeenCalled();
    });

  it('does not redirect a session lost after the page settled authenticated',
    async () => {
      meStatus = 200;
      const auth = useAuth();
      await auth.refresh();
      expect(auth.authState.value).toBe('authenticated');

      signedInMiddleware(
        route('/app/settings/sessions'),
        route('/app/settings/sessions'),
      );
      expect(vi.mocked(navigateTo)).not.toHaveBeenCalled();

      meStatus = 401;
      await auth.refresh();
      expect(auth.authState.value).toBe('anonymous');
      await flushPromises();
      expect(vi.mocked(navigateTo)).not.toHaveBeenCalled();
    });

  it('sends an anonymous /app/new visitor to register with next', async () => {
    const auth = useAuth();
    await auth.refresh();
    expect(auth.authState.value).toBe('anonymous');

    signedInMiddleware(
      route('/app/new', '/app/new?sample=engineer-compact&lng=en'),
      route('/templates'),
    );

    expect(vi.mocked(navigateTo)).toHaveBeenCalledWith(
      `/register?next=${
        encodeURIComponent('/app/new?sample=engineer-compact&lng=en')
      }`,
      { replace: true },
    );
  });
});
