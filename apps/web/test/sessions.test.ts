import { beforeEach, describe, expect, it, vi } from 'vitest';
import {
  mockNuxtImport,
  mountSuspended,
  registerEndpoint,
} from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { createError, readRawBody, setResponseStatus } from 'h3';
import SessionsPage from '../app/pages/app/settings/sessions.vue';
import { registerCapabilities } from './support/capabilities';

registerCapabilities();

// revoke-all (on success) and the current-session "Log out" button both
// navigate away via `navigateTo` — stub it so tests can assert on the
// target without a real page transition tearing the mounted wrapper down
// mid-test. `mockNuxtImport`'s factory is hoisted above any top-level
// `const`, so the mock function has to be created inside it; `navigateTo`
// itself becomes accessible as an auto-import in this file afterward.
mockNuxtImport('navigateTo', () => vi.fn());

// The registerEndpoint mock's event doesn't normalize header casing the way
// a real Node request does, and h3's own `getRequestHeader` disagrees with
// the mocked event shape across the versions pinned here — so read the
// mocked request headers directly, case-insensitively.
interface MockEvent {
  node?: { req?: { headers?: Record<string, string> } };
}

function requestHeader(event: MockEvent, name: string): string | undefined {
  const headers = event.node?.req?.headers ?? {};
  const key = Object.keys(headers).find(
    (k) => k.toLowerCase() === name.toLowerCase(),
  );
  return key ? headers[key] : undefined;
}

const meData = {
  user: {
    id: 'user-1',
    email: 'demo@example.com',
    name: 'Demo User',
    avatarKey: null,
    hasPassword: false,
  },
  csrfToken: 'test-csrf-token',
  identities: [{ provider: 'google' }],
};

const sessionsData = [
  {
    id: 'sess-1',
    createdAt: '2026-07-01T00:00:00Z',
    lastSeenAt: '2026-08-01T00:00:00Z',
    ua: 'Chrome on macOS',
    ip: '203.0.113.10',
    current: true,
  },
  {
    id: 'sess-2',
    createdAt: '2026-06-01T00:00:00Z',
    lastSeenAt: '2026-07-15T00:00:00Z',
    ua: 'Firefox on Linux',
    ip: '203.0.113.20',
    current: false,
  },
];

registerEndpoint('/api/v1/me', () => ({ data: meData }));
registerEndpoint('/api/v1/sessions', () => ({ data: sessionsData }));

describe('sessions.vue', () => {
  beforeEach(() => {
    vi.mocked(navigateTo).mockClear();
  });

  it('lists sessions and treats the current one distinctly', async () => {
    const wrapper = await mountSuspended(SessionsPage);
    await flushPromises();

    const rows = wrapper.findAll('[data-testid^="session-row-"]');
    expect(rows).toHaveLength(2);

    const currentRow = wrapper.get('[data-testid="session-row-sess-1"]');
    // Revoking your own current session is logout, not a generic device
    // removal — no identical "Revoke" action on the current row.
    expect(
      currentRow.findAll('button').some((b) => b.text() === 'Revoke'),
    ).toBe(false);
    expect(currentRow.text()).toContain('This device');

    const otherRow = wrapper.get('[data-testid="session-row-sess-2"]');
    expect(otherRow.get('[data-testid="revoke-button"]').text()).toBe(
      'Revoke',
    );
  });

  it('revokes another session, sending the CSRF header', async () => {
    let receivedHeader: string | undefined;
    registerEndpoint('/api/v1/sessions/sess-2', {
      method: 'DELETE',
      handler: (event) => {
        receivedHeader = requestHeader(event, 'x-csrf-token');
        setResponseStatus(event, 204);
        return null;
      },
    });

    const wrapper = await mountSuspended(SessionsPage);
    await flushPromises();

    await wrapper
      .get('[data-testid="session-row-sess-2"] [data-testid="revoke-button"]')
      .trigger('click');
    await flushPromises();

    expect(receivedHeader).toBe('test-csrf-token');
  });

  it('treats a 404 revoke as an already-gone row, not an error', async () => {
    let deleteAttempted = false;
    registerEndpoint('/api/v1/sessions/sess-2', {
      method: 'DELETE',
      handler: () => {
        deleteAttempted = true;
        throw createError({ statusCode: 404 });
      },
    });

    const wrapper = await mountSuspended(SessionsPage);
    await flushPromises();

    await wrapper
      .get('[data-testid="session-row-sess-2"] [data-testid="revoke-button"]')
      .trigger('click');
    await flushPromises();
    await flushPromises();

    expect(deleteAttempted).toBe(true);
    // An already-gone session is a stale row, not a user-visible error.
    expect(wrapper.find('[data-testid="revoke-error"]').exists()).toBe(
      false,
    );
  });

  it('shows an error dialog for a non-404 revoke failure', async () => {
    registerEndpoint('/api/v1/sessions/sess-2', {
      method: 'DELETE',
      handler: () => {
        throw createError({ statusCode: 500 });
      },
    });

    const wrapper = await mountSuspended(SessionsPage);
    await flushPromises();

    await wrapper
      .get('[data-testid="session-row-sess-2"] [data-testid="revoke-button"]')
      .trigger('click');
    await flushPromises();
    await flushPromises();

    // Contrast with the 404 case above: a genuine failure (not "already
    // gone") does surface an error, proving the 404 assertion above isn't
    // vacuously true.
    expect(wrapper.get('[data-testid="revoke-error"]').text()).toContain(
      'Could not revoke',
    );
  });

  it(
    'shows the reauth prompt (not a generic error) when a single-session '
    + 'revoke requires recent reauth',
    async () => {
      let deleteAttempts = 0;
      registerEndpoint('/api/v1/sessions/sess-2', {
        method: 'DELETE',
        handler: (event) => {
          deleteAttempts += 1;
          setResponseStatus(event, 403);
          return { error: { code: 'reauth_required', message: 'x' } };
        },
      });

      const wrapper = await mountSuspended(SessionsPage);
      await flushPromises();

      await wrapper
        .get(
          '[data-testid="session-row-sess-2"] [data-testid="revoke-button"]',
        )
        .trigger('click');
      await flushPromises();
      await flushPromises();

      // A boolean flag can't tell "attempted once" from "attempted
      // twice" — mutate()'s CSRF self-heal must not treat this 403 as
      // csrf_rejected and retry it (that would double-fire a mutation
      // the server already refused for an unrelated reason).
      expect(deleteAttempts).toBe(1);

      expect(wrapper.find('[data-testid="revoke-error"]').exists()).toBe(
        false,
      );
      const prompt = wrapper.get('[data-testid="reauth-prompt"]');
      // "action" reason copy, not the link-specific wording — revoking a
      // session has nothing to do with linking a provider.
      expect(prompt.text()).toContain('then try again');
      expect(prompt.text()).not.toContain('link a new provider');
    },
  );

  it('starts linking with a bodiless CSRF POST and follows the provider URL',
    async () => {
      let meCalls = 0;
      registerEndpoint('/api/v1/me', {
        handler: () => {
          meCalls += 1;
          return { data: meData };
        },
      });
      let receivedMethod: string | undefined;
      let receivedHeader: string | undefined;
      let receivedContentType: string | undefined;
      let receivedBody: string | undefined;
      registerEndpoint('/api/v1/auth/github/start', {
        method: 'POST',
        handler: async (event) => {
          receivedMethod = event.method;
          receivedHeader = requestHeader(event, 'x-csrf-token');
          receivedContentType = requestHeader(event, 'content-type');
          receivedBody = await readRawBody(event);
          return {
            data: {
              authorizeUrl:
              'https://github.com/login/oauth/authorize?state=test-state',
            },
          };
        },
      });

      const wrapper = await mountSuspended(SessionsPage);
      await flushPromises();

      const addButton = wrapper.get('[data-testid="add-provider-button"]');
      const callsBeforeClick = meCalls;
      await addButton.trigger('click');
      await flushPromises();

      // Refreshes identities/csrfToken before offering link targets, so it
      // doesn't act on stale state.
      expect(meCalls).toBeGreaterThan(callsBeforeClick);

      // meData only has a `google` identity linked, so github/linkedin are
      // offered — and google (already linked) is not.
      const linkButtons = wrapper
        .findAll('button')
        .filter((button) => button.text().startsWith('Link '));
      expect(linkButtons.map((button) => button.text())).toEqual([
        'Link github',
        'Link linkedin',
      ]);

      await linkButtons[0]!.trigger('click');
      await flushPromises();
      await flushPromises();

      expect(receivedMethod).toBe('POST');
      expect(receivedHeader).toBe('test-csrf-token');
      expect(receivedContentType).toBeUndefined();
      expect(receivedBody ?? '').toBe('');
      expect(vi.mocked(navigateTo)).toHaveBeenCalledWith(
        'https://github.com/login/oauth/authorize?state=test-state',
        { external: true },
      );
    });

  it(
    'prompts with an existing provider when a link start requires reauth',
    async () => {
      let linkAttempts = 0;
      registerEndpoint('/api/v1/auth/github/start', {
        method: 'POST',
        handler: (event) => {
          linkAttempts += 1;
          setResponseStatus(event, 403);
          return { error: { code: 'reauth_required', message: 'x' } };
        },
      });

      const wrapper = await mountSuspended(SessionsPage);
      await flushPromises();

      await wrapper.get('[data-testid="add-provider-button"]').trigger('click');
      await flushPromises();
      const githubButton = wrapper
        .findAll('button')
        .find((button) => button.text() === 'Link github');
      expect(githubButton).toBeDefined();

      await githubButton!.trigger('click');
      await flushPromises();
      await flushPromises();

      expect(linkAttempts).toBe(1);
      const prompt = wrapper.get('[data-testid="reauth-prompt"]');
      expect(prompt.text()).toContain('link a new provider');
      expect(prompt.get('button').text()).toBe('Sign in again with google');
      expect(wrapper.find('[data-testid="link-error"]').exists()).toBe(false);
      expect(vi.mocked(navigateTo)).not.toHaveBeenCalled();
    },
  );

  it('shows nothing extra when there is no reauth error', async () => {
    const wrapper = await mountSuspended(SessionsPage);
    await flushPromises();

    expect(wrapper.find('[data-testid="reauth-prompt"]').exists()).toBe(
      false,
    );
    expect(wrapper.find('[data-testid="link-error"]').exists()).toBe(false);
  });

  it(
    'prompts to reauthenticate with an existing provider when the '
    + 'link start bounces back with ?error=reauth_required',
    async () => {
      const wrapper = await mountSuspended(SessionsPage, {
        route: '/app/settings/sessions?error=reauth_required',
      });
      await flushPromises();

      const prompt = wrapper.get('[data-testid="reauth-prompt"]');
      // "link" reason copy, distinct from the "action" copy the
      // revoke/revoke-all tests above use.
      expect(prompt.text()).toContain('link a new provider');

      // meData's only linked identity is `google` — reauth targets that
      // existing provider, never the not-yet-linked one being added.
      const reauthButton = prompt.get('button');
      expect(reauthButton.text()).toBe('Sign in again with google');
    },
  );

  it('shows a generic banner for ?error=identity_already_linked',
    async () => {
      const wrapper = await mountSuspended(SessionsPage, {
        route: '/app/settings/sessions?error=identity_already_linked',
      });
      await flushPromises();

      const banner = wrapper.get('[data-testid="link-error"]');
      expect(banner.text()).toContain('already linked');
      expect(wrapper.find('[data-testid="reauth-prompt"]').exists()).toBe(
        false,
      );
    });

  it(
    'falls back to a generic link-error message for an unrecognized '
    + 'code, and does not resolve a prototype property',
    async () => {
      const unrecognized = await mountSuspended(SessionsPage, {
        route: '/app/settings/sessions?error=some_unrecognized_code',
      });
      await flushPromises();
      expect(
        unrecognized.get('[data-testid="link-error"]').text(),
      ).toContain('Something went wrong');

      // A plain `linkErrorMessages[code]` lookup resolves inherited
      // properties too — `?error=constructor` would otherwise render
      // `Object`'s constructor function instead of falling back.
      const prototypeAttempt = await mountSuspended(SessionsPage, {
        route: '/app/settings/sessions?error=constructor',
      });
      await flushPromises();
      expect(
        prototypeAttempt.get('[data-testid="link-error"]').text(),
      ).toContain('Something went wrong');
    },
  );

  it('sends the CSRF header when logging out everywhere', async () => {
    let receivedHeader: string | undefined;
    let receivedMethod: string | undefined;
    registerEndpoint('/api/v1/sessions', {
      method: 'DELETE',
      handler: (event) => {
        receivedMethod = event.method;
        receivedHeader = requestHeader(event, 'x-csrf-token');
        setResponseStatus(event, 204);
        return null;
      },
    });

    const wrapper = await mountSuspended(SessionsPage);
    await flushPromises();

    await wrapper.get('[data-testid="revoke-all-button"]').trigger('click');
    await flushPromises();

    expect(receivedMethod).toBe('DELETE');
    expect(receivedHeader).toBe('test-csrf-token');
  });

  it(
    'navigates to /login on a successful revoke-all, without '
    + 'refetching /me or the session list afterward',
    async () => {
      let meGetCalls = 0;
      registerEndpoint('/api/v1/me', {
        method: 'GET',
        handler: () => {
          meGetCalls += 1;
          return { data: meData };
        },
      });
      let sessionsGetCalls = 0;
      registerEndpoint('/api/v1/sessions', {
        method: 'GET',
        handler: () => {
          sessionsGetCalls += 1;
          return { data: sessionsData };
        },
      });
      registerEndpoint('/api/v1/sessions', {
        method: 'DELETE',
        handler: (event) => {
          setResponseStatus(event, 204);
          return null;
        },
      });

      const wrapper = await mountSuspended(SessionsPage);
      await flushPromises();

      const meGetCallsAtMount = meGetCalls;
      const sessionsGetCallsAtMount = sessionsGetCalls;

      await wrapper.get('[data-testid="revoke-all-button"]').trigger('click');
      await flushPromises();
      await flushPromises();

      // Revoke-all destroys the current session too (Clear-Site-Data) —
      // there is nothing left to refetch, only somewhere else to go.
      expect(vi.mocked(navigateTo)).toHaveBeenCalledWith('/login');
      expect(meGetCalls).toBe(meGetCallsAtMount);
      expect(sessionsGetCalls).toBe(sessionsGetCallsAtMount);
    },
  );

  it(
    'shows the reauth prompt when revoke-all requires recent reauth, '
    + 'without touching any session row',
    async () => {
      let revokeAllAttempts = 0;
      registerEndpoint('/api/v1/sessions', {
        method: 'DELETE',
        handler: (event) => {
          revokeAllAttempts += 1;
          setResponseStatus(event, 403);
          return { error: { code: 'reauth_required', message: 'x' } };
        },
      });

      const wrapper = await mountSuspended(SessionsPage);
      await flushPromises();
      expect(wrapper.find('[data-testid="reauth-prompt"]').exists()).toBe(
        false,
      );

      await wrapper.get('[data-testid="revoke-all-button"]').trigger('click');
      await flushPromises();
      await flushPromises();

      // A boolean flag can't tell "attempted once" from "attempted
      // twice" — mutate()'s CSRF self-heal must not treat this 403 as
      // csrf_rejected and retry it (that would double-fire a mutation
      // the server already refused for an unrelated reason).
      expect(revokeAllAttempts).toBe(1);
      const prompt = wrapper.get('[data-testid="reauth-prompt"]');
      expect(prompt.text()).toContain('then try again');
      expect(prompt.text()).not.toContain('link a new provider');
      expect(wrapper.find('[data-testid="revoke-error"]').exists()).toBe(
        false,
      );
      // Sensitive-op rejection, not a generic failure — and definitely
      // not a silent success.
      expect(vi.mocked(navigateTo)).not.toHaveBeenCalled();
    },
  );

  it('shows an error banner for a non-reauth revoke-all failure',
    async () => {
      registerEndpoint('/api/v1/sessions', {
        method: 'DELETE',
        handler: () => {
          throw createError({ statusCode: 500 });
        },
      });

      const wrapper = await mountSuspended(SessionsPage);
      await flushPromises();

      await wrapper.get('[data-testid="revoke-all-button"]').trigger('click');
      await flushPromises();
      await flushPromises();

      expect(wrapper.get('[data-testid="revoke-error"]').text()).toContain(
        'Could not log out everywhere',
      );
      expect(
        wrapper.find('[data-testid="reauth-prompt"]').exists(),
      ).toBe(false);
      expect(vi.mocked(navigateTo)).not.toHaveBeenCalled();
    });

  describe('password settings integration', () => {
    it('refreshes /me and the session list after a successful add',
      async () => {
        let meCalls = 0;
        registerEndpoint('/api/v1/me', {
          handler: () => {
            meCalls += 1;
            return { data: meData };
          },
        });
        let sessionsGetCalls = 0;
        registerEndpoint('/api/v1/sessions', {
          method: 'GET',
          handler: () => {
            sessionsGetCalls += 1;
            return { data: sessionsData };
          },
        });
        registerEndpoint('/api/v1/me/password', {
          method: 'PUT',
          handler: (event) => {
            setResponseStatus(event, 204);
            return null;
          },
        });

        const wrapper = await mountSuspended(SessionsPage);
        await flushPromises();

        expect(wrapper.get('[data-testid="password-status"]').text()).toBe(
          'No password set.',
        );

        const meCallsAtMount = meCalls;
        const sessionsCallsAtMount = sessionsGetCalls;

        await wrapper.get('[data-testid="password-action"]').trigger('click');
        await flushPromises();
        await wrapper.get('#password-new').setValue('new-secret');
        await wrapper.get('#password-new-confirm').setValue('new-secret');
        await wrapper.get('form').trigger('submit');
        await flushPromises();
        await flushPromises();

        // The parent's `updated` handler refetches /me and the device list
        // (a successful add/change replaces the current session).
        expect(meCalls).toBeGreaterThan(meCallsAtMount);
        expect(sessionsGetCalls).toBeGreaterThan(sessionsCallsAtMount);
      });

    it('sets a password through an authenticated PUT with JSON and CSRF',
      async () => {
        let receivedMethod: string | undefined;
        let receivedHeader: string | undefined;
        let receivedContentType: string | undefined;
        let receivedBody: string | undefined;
        registerEndpoint('/api/v1/me/password', {
          method: 'PUT',
          handler: async (event) => {
            receivedMethod = event.method;
            receivedHeader = requestHeader(event, 'x-csrf-token');
            receivedContentType = requestHeader(event, 'content-type');
            receivedBody = await readRawBody(event);
            setResponseStatus(event, 204);
            return null;
          },
        });

        const wrapper = await mountSuspended(SessionsPage);
        await flushPromises();

        await wrapper.get('[data-testid="password-action"]').trigger('click');
        await flushPromises();
        await wrapper.get('#password-new').setValue('new-secret');
        await wrapper.get('#password-new-confirm').setValue('new-secret');
        await wrapper.get('form').trigger('submit');
        await flushPromises();

        expect(receivedMethod).toBe('PUT');
        expect(receivedHeader).toBe('test-csrf-token');
        expect(receivedContentType).toBe('application/json');
        expect(receivedBody).toBe('{"password":"new-secret"}');
      });

    it('starts a provider reauth round trip when an add needs reauth',
      async () => {
        registerEndpoint('/api/v1/me/password', {
          method: 'PUT',
          handler: (event) => {
            setResponseStatus(event, 403);
            return { error: { code: 'reauth_required', message: 'x' } };
          },
        });
        let receivedMethod: string | undefined;
        registerEndpoint('/api/v1/auth/google/start', {
          method: 'POST',
          handler: (event) => {
            receivedMethod = event.method;
            return {
              data: {
                authorizeUrl:
                'https://accounts.google.com/o/oauth2/v2/auth?state=test',
              },
            };
          },
        });

        const wrapper = await mountSuspended(SessionsPage);
        await flushPromises();

        await wrapper.get('[data-testid="password-action"]').trigger('click');
        await flushPromises();
        await wrapper.get('#password-new').setValue('new-secret');
        await wrapper.get('#password-new-confirm').setValue('new-secret');
        await wrapper.get('form').trigger('submit');
        await flushPromises();

        // The linked provider is offered; no email is shown.
        expect(
          wrapper.find('[data-testid="password-provider-reauth-google"]')
            .exists(),
        ).toBe(true);
        expect(
          wrapper.find('[data-testid="password-provider-reauth-google"]')
            .text(),
        ).toBe('Continue with Google');

        await wrapper
          .get('[data-testid="password-provider-reauth-google"]')
          .trigger('click');
        await flushPromises();
        await flushPromises();

        expect(receivedMethod).toBe('POST');
        expect(vi.mocked(navigateTo)).toHaveBeenCalledWith(
          'https://accounts.google.com/o/oauth2/v2/auth?state=test',
          { external: true },
        );
      });
  });
});

describe('sessions.vue capability gating', () => {
  beforeEach(() => {
    clearNuxtData();
  });

  it('hides the provider block when providerLogin is false', async () => {
    registerCapabilities({ providerLogin: false, agentAccess: true });
    const wrapper = await mountSuspended(SessionsPage);
    await flushPromises();
    expect(wrapper.text()).not.toContain('Add another sign-in provider');
  });

  it(
    'hides Connected agents and never requests the grant list when agentAccess '
    + 'is false',
    async () => {
      let agentRequests = 0;
      registerEndpoint('/api/v1/me/agents', () => {
        agentRequests += 1;
        return { data: { grants: [] } };
      });
      registerCapabilities({ providerLogin: true, agentAccess: false });
      const wrapper = await mountSuspended(SessionsPage);
      await flushPromises();
      expect(wrapper.text()).not.toContain('Connected agents');
      expect(agentRequests).toBe(0);
      // Sessions and password remain.
      expect(wrapper.text()).toContain('Signed-in devices');
      expect(wrapper.text()).toContain('Password');
    },
  );

  it('hides provider and agent blocks while capabilities are pending',
    async () => {
      let release!: (body: unknown) => void;
      registerEndpoint('/api/v1/me/agents', () => ({
        data: { grants: [] },
      }));
      registerEndpoint('/api/v1/capabilities', () => new Promise((resolve) => {
        release = () => resolve({
          data: { providerLogin: true, agentAccess: true },
        });
      }));
      const wrapper = await mountSuspended(SessionsPage);
      expect(wrapper.text()).not.toContain('Add another sign-in provider');
      expect(wrapper.text()).not.toContain('Connected agents');
      release({
        data: { providerLogin: true, agentAccess: true },
      });
      await flushPromises();
      await flushPromises();
      expect(wrapper.text()).toContain('Add another sign-in provider');
      expect(wrapper.text()).toContain('Connected agents');
    });

  it('shows both blocks when both flags are true', async () => {
    registerEndpoint('/api/v1/me/agents', () => ({ data: { grants: [] } }));
    registerCapabilities({ providerLogin: true, agentAccess: true });
    const wrapper = await mountSuspended(SessionsPage);
    await flushPromises();
    expect(wrapper.text()).toContain('Add another sign-in provider');
    expect(wrapper.text()).toContain('Connected agents');
  });
});
