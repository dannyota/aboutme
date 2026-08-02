import { describe, expect, it } from 'vitest';
import { mountSuspended, registerEndpoint } from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { createError } from 'h3';
import SessionsPage from '../app/pages/app/settings/sessions.vue';

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
        return { data: { revoked: true } };
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
    // DD-C5: DELETE of an already-gone session returns 404 uniformly, and
    // the UI treats that as "stale row" rather than surfacing an error.
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

  it('offers to link providers the user has not connected yet', async () => {
    let meCalls = 0;
    registerEndpoint('/api/v1/me', {
      handler: () => {
        meCalls += 1;
        return { data: meData };
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
    const github = wrapper.get(
      'a[href="/api/v1/auth/github/start?purpose=link"]',
    );
    const linkedin = wrapper.get(
      'a[href="/api/v1/auth/linkedin/start?purpose=link"]',
    );
    expect(github.element.tagName).toBe('A');
    expect(linkedin.element.tagName).toBe('A');
    expect(
      wrapper.find('a[href="/api/v1/auth/google/start?purpose=link"]')
        .exists(),
    ).toBe(false);
  });

  it('shows nothing extra when there is no reauth error', async () => {
    const wrapper = await mountSuspended(SessionsPage);
    await flushPromises();

    expect(wrapper.find('[data-testid="reauth-prompt"]').exists()).toBe(
      false,
    );
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
      expect(prompt.text()).toContain('confirm it\'s you');

      // meData's only linked identity is `google` — reauth targets that
      // existing provider, never the not-yet-linked one being added.
      const reauthLink = prompt.get(
        'a[href="/api/v1/auth/google/start?purpose=reauth"]',
      );
      expect(reauthLink.element.tagName).toBe('A');
    },
  );

  it('logs out everywhere via revoke-all, sending the CSRF header',
    async () => {
      let receivedHeader: string | undefined;
      let receivedMethod: string | undefined;
      registerEndpoint('/api/v1/sessions/revoke-all', {
        method: 'POST',
        handler: (event) => {
          receivedMethod = event.method;
          receivedHeader = requestHeader(event, 'x-csrf-token');
          return { data: { revoked: true } };
        },
      });

      const wrapper = await mountSuspended(SessionsPage);
      await flushPromises();

      await wrapper.get('[data-testid="revoke-all-button"]').trigger('click');
      await flushPromises();

      expect(receivedMethod).toBe('POST');
      expect(receivedHeader).toBe('test-csrf-token');
    });
});
