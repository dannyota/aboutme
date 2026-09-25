import {
  beforeEach,
  describe,
  expect,
  it,
  vi,
  type MockInstance,
} from 'vitest';
import { registerEndpoint } from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { setResponseStatus } from 'h3';
import type { RouteLocationNormalized, Router } from 'vue-router';
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
const SAMPLE_PATH = '/app/new?sample=engineer-compact&lng=en';
const SAMPLE_REGISTER = `/register?next=${encodeURIComponent(SAMPLE_PATH)}`;

// The shared signed-out route guard (docs/design/web.md, Application
// surfaces). A router push to an app route runs the registered global guard
// too; both share one pending watcher.
describe('signed-in.global middleware', () => {
  let replace: MockInstance<Router['replace']>;

  beforeEach(async () => {
    meStatus = 401;
    meGate = Promise.resolve();
    vi.restoreAllMocks();
    await useRouter().push('/');
    clearNuxtData();
    // These tests call the guard directly or navigate with push, never
    // replace, so every replace is a redirect the guard sent.
    replace = vi.spyOn(useRouter(), 'replace').mockResolvedValue(undefined);
  });

  it('does nothing for a path that requires no session', async () => {
    const result = signedInMiddleware(
      route('/templates'),
      route('/templates'),
    );
    expect(result).toBeUndefined();
    await flushPromises();
    expect(replace).not.toHaveBeenCalled();
  });

  it('re-reads a rejected session on navigation, then redirects',
    async () => {
      const auth = useAuth();
      await auth.refresh();
      expect(auth.authState.value).toBe('anonymous');

      holdMe();
      await useRouter().push('/app/settings/sessions');
      expect(auth.authState.value).toBe('loading');
      expect(replace).not.toHaveBeenCalled();

      releaseMe();
      await vi.waitFor(() => {
        expect(replace).toHaveBeenCalledWith(SESSIONS_LOGIN);
      });
    });

  it('redirects to the navigation target when the read settles first',
    async () => {
      // The router still shows '/': the navigation has not committed.
      holdMe();
      signedInMiddleware(route('/app/settings/sessions'), route('/'));
      expect(replace).not.toHaveBeenCalled();

      releaseMe();
      await vi.waitFor(() => {
        expect(replace).toHaveBeenCalledWith(SESSIONS_LOGIN);
      });
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
      expect(replace).not.toHaveBeenCalled();

      releaseMe();
      await vi.waitFor(() => {
        expect(replace).toHaveBeenCalledWith(SESSIONS_LOGIN);
      });
      await flushPromises();
      expect(replace).toHaveBeenCalledTimes(1);
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
      expect(replace).not.toHaveBeenCalled();
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
      expect(replace).not.toHaveBeenCalled();
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
      expect(replace).not.toHaveBeenCalled();

      meStatus = 401;
      await auth.refresh();
      expect(auth.authState.value).toBe('anonymous');
      await flushPromises();
      expect(replace).not.toHaveBeenCalled();
    });

  it('sends an anonymous /app/new visitor to register with next', async () => {
    holdMe();
    await useRouter().push(SAMPLE_PATH);
    releaseMe();

    await vi.waitFor(() => {
      expect(replace).toHaveBeenCalledWith(SAMPLE_REGISTER);
    });
  });

  it('redirects when the read settles before the navigation commits',
    async () => {
      // A link from a gallery page: Nuxt marks the navigation as processing
      // middleware from beforeEach until afterEach, while the /app/new page
      // chunk loads, and the 401 lands inside that window.
      const nuxtApp = useNuxtApp();
      holdMe();
      nuxtApp._processingMiddleware = true;
      try {
        signedInMiddleware(
          route('/app/new', SAMPLE_PATH),
          route('/templates/engineer-compact'),
        );
        releaseMe();
        await vi.waitFor(() => {
          expect(replace).toHaveBeenCalledWith(SAMPLE_REGISTER);
        });
      } finally {
        delete nuxtApp._processingMiddleware;
      }
    });
});
