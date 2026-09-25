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
mockNuxtImport('navigateTo', () => vi.fn());
registerEndpoint('/api/v1/me', (event) => {
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

describe('signed-in.global middleware', () => {
  beforeEach(() => {
    meStatus = 401;
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
      await flushPromises();
      expect(auth.authState.value).toBe('anonymous');

      signedInMiddleware(
        route('/app/settings/sessions'),
        route('/app/settings/sessions'),
      );

      expect(vi.mocked(navigateTo)).toHaveBeenCalledWith(
        '/login?next=%2Fapp%2Fsettings%2Fsessions',
        { replace: true },
      );
    });

  it(
    'does not block navigation while loading, then redirects once settled '
      + 'anonymous',
    async () => {
      const router = useRouter();
      await router.push('/app/settings/sessions');

      const result = signedInMiddleware(
        route('/app/settings/sessions'),
        route('/'),
      );
      expect(result).toBeUndefined();
      expect(vi.mocked(navigateTo)).not.toHaveBeenCalled();

      await flushPromises();

      expect(vi.mocked(navigateTo)).toHaveBeenCalledTimes(1);
      expect(vi.mocked(navigateTo)).toHaveBeenCalledWith(
        '/login?next=%2Fapp%2Fsettings%2Fsessions',
        { replace: true },
      );
    },
  );

  it('does not redirect when the settling read lands authenticated',
    async () => {
      meStatus = 200;
      const router = useRouter();
      await router.push('/app/settings/sessions');

      const result = signedInMiddleware(
        route('/app/settings/sessions'),
        route('/'),
      );
      expect(result).toBeUndefined();

      await flushPromises();

      expect(vi.mocked(navigateTo)).not.toHaveBeenCalled();
    });

  it(
    'does not redirect when the current route no longer requires a session '
      + 'once the read settles',
    async () => {
      const router = useRouter();
      await router.push('/app/settings/sessions');

      signedInMiddleware(route('/app/settings/sessions'), route('/'));
      await router.push('/templates');
      await flushPromises();

      expect(vi.mocked(navigateTo)).not.toHaveBeenCalled();
    },
  );

  it('does not redirect a session lost after the page settled authenticated',
    async () => {
      meStatus = 200;
      const auth = useAuth();
      await flushPromises();
      expect(auth.authState.value).toBe('authenticated');

      signedInMiddleware(
        route('/app/settings/sessions'),
        route('/app/settings/sessions'),
      );
      expect(vi.mocked(navigateTo)).not.toHaveBeenCalled();

      meStatus = 401;
      await auth.refresh();
      expect(auth.authState.value).toBe('anonymous');
      expect(vi.mocked(navigateTo)).not.toHaveBeenCalled();
    });

  it('sends an anonymous /app/new visitor to register with next', async () => {
    const auth = useAuth();
    await flushPromises();
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
