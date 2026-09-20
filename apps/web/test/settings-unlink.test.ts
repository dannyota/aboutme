import { beforeEach, describe, expect, it, vi } from 'vitest';
import {
  mockNuxtImport,
  mountSuspended,
  registerEndpoint,
} from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { readRawBody, setResponseStatus, type H3Event } from 'h3';
import SessionsPage from '../app/pages/app/settings/sessions.vue';
import { registerCapabilities } from './support/capabilities';
import { setSiteLocale } from './support/locale';

mockNuxtImport('navigateTo', () => vi.fn());

interface Identity {
  id: string;
  provider: 'google' | 'github' | 'linkedin';
  createdAt: string;
}

const now = new Date('2026-09-18T12:00:00Z');
let linked: Identity[] = [];
let hasPassword = true;
let meCalls = 0;
let deletes: { id: string; csrf: string | undefined; body: string }[] = [];
let respondToDelete: (event: H3Event, id: string) => unknown = () => null;
let reauthBodies: string[] = [];

function identity(
  id: string,
  provider: Identity['provider'],
  createdAt = '2026-09-18T03:00:00Z',
): Identity {
  return { id, provider, createdAt };
}

registerEndpoint('/api/v1/me', () => {
  meCalls += 1;
  return {
    data: {
      user: {
        id: 'user-1',
        email: 'demo@example.com',
        name: 'Demo User',
        avatarKey: null,
        hasPassword,
      },
      csrfToken: 'csrf',
      identities: linked,
    },
  };
});
interface SessionRow {
  id: string;
  createdAt: string;
  lastSeenAt: string;
  ua: string | null;
  ip: string | null;
  current: boolean;
}

function session(id: string, current: boolean): SessionRow {
  return {
    id,
    createdAt: '2026-09-18T01:00:00Z',
    lastSeenAt: '2026-09-18T11:00:00Z',
    ua: null,
    ip: null,
    current,
  };
}

let sessionRows: SessionRow[] = [];
let revokedSessions: string[] = [];
registerEndpoint('/api/v1/sessions', () => ({ data: sessionRows }));
for (const id of ['s-other-1', 's-other-2', 's-current']) {
  registerEndpoint(`/api/v1/sessions/${id}`, {
    method: 'DELETE',
    handler: (event) => {
      revokedSessions.push(id);
      sessionRows = sessionRows.filter((row) => row.id !== id);
      setResponseStatus(event, 204);
      return null;
    },
  });
}
registerEndpoint('/api/v1/auth/password/reauth', {
  method: 'POST',
  handler: async (event) => {
    reauthBodies.push((await readRawBody(event)) ?? '');
    setResponseStatus(event, 204);
    return null;
  },
});
for (const id of ['id-a', 'id-b']) {
  registerEndpoint(`/api/v1/me/identities/${id}`, {
    method: 'DELETE',
    handler: async (event) => {
      deletes.push({
        id,
        csrf: csrfHeader(event),
        body: (await readRawBody(event)) ?? '',
      });
      return respondToDelete(event, id);
    },
  });
}

function csrfHeader(event: H3Event): string | undefined {
  const headers = event.node.req.headers as Record<string, string | undefined>;
  const key = Object.keys(headers).find(
    (name) => name.toLowerCase() === 'x-csrf-token',
  );
  return key ? headers[key] : undefined;
}

function removeOnDelete(event: H3Event, id: string): null {
  linked = linked.filter((item) => item.id !== id);
  setResponseStatus(event, 204);
  return null;
}

type Wrapper = Awaited<ReturnType<typeof mountSuspended>>;

async function mountSettings(
  providers: readonly string[] = ['google'],
  attachTo?: Element,
): Promise<Wrapper> {
  registerCapabilities({
    providerLogin: providers.length > 0,
    agentAccess: false,
    providers,
  });
  const wrapper = await mountSuspended(SessionsPage, {
    attachTo,
    props: { now },
  });
  await flushPromises();
  return wrapper;
}

function unlinkButton(wrapper: Wrapper, id: string) {
  return wrapper.get(
    `[data-identity-id="${id}"] [data-testid="unlink-button"]`,
  );
}

async function confirmUnlink(wrapper: Wrapper, id: string): Promise<void> {
  await unlinkButton(wrapper, id).trigger('click');
  await flushPromises();
  const confirm = document.body.querySelector<HTMLElement>(
    '[data-action="confirm-unlink"]',
  );
  expect(confirm?.textContent).toContain(
    linked.find((item) => item.id === id)?.provider === 'github'
      ? 'GitHub'
      : 'Google',
  );
  confirm?.click();
  await flushPromises();
  await flushPromises();
}

beforeEach(() => {
  setSiteLocale('en');
  linked = [];
  hasPassword = true;
  meCalls = 0;
  deletes = [];
  reauthBodies = [];
  respondToDelete = removeOnDelete;
  sessionRows = [];
  revokedSessions = [];
  clearNuxtData();
});

describe('unlinking a provider identity', () => {
  it('renders linked identity copy and UTC dates in Vietnamese', async () => {
    setSiteLocale('vi');
    linked = [identity('id-a', 'google')];

    const wrapper = await mountSettings();

    expect(wrapper.get('[data-identity-id="id-a"]').text()).toContain(
      'Đã liên kết ngày 18 tháng 9, 2026',
    );
    expect(unlinkButton(wrapper, 'id-a').text()).toBe('Hủy liên kết');
  });

  it('keeps the selected identity when the unlink dialog changes locale',
    async () => {
      linked = [
        identity('id-a', 'google'),
        identity('id-b', 'github'),
      ];
      const wrapper = await mountSettings(['google', 'github']);

      await unlinkButton(wrapper, 'id-b').trigger('click');
      await flushPromises();
      const dialog = document.body.querySelector<HTMLElement>(
        '[role="alertdialog"]',
      );
      expect(dialog?.textContent).toContain('Unlink GitHub?');

      dialog?.querySelector<HTMLElement>('[aria-label="Tiếng Việt"]')?.click();
      await flushPromises();

      expect(dialog?.textContent).toContain('Hủy liên kết GitHub?');
      dialog?.querySelector<HTMLElement>(
        '[data-action="confirm-unlink"]',
      )?.click();
      await flushPromises();
      await flushPromises();

      expect(deletes).toEqual([{ id: 'id-b', csrf: 'csrf', body: '' }]);
      expect(wrapper.find('[data-identity-id="id-a"]').exists()).toBe(true);
      expect(wrapper.find('[data-identity-id="id-b"]').exists()).toBe(false);
    });

  it('shows a safe Vietnamese message for an unknown unlink failure',
    async () => {
      setSiteLocale('vi');
      linked = [identity('id-a', 'google')];
      respondToDelete = (event) => {
        setResponseStatus(event, 500);
        return {
          error: { code: 'unexpected', message: 'private backend detail' },
        };
      };
      const wrapper = await mountSettings();

      await confirmUnlink(wrapper, 'id-a');

      const error = wrapper.get('[data-testid="unlink-error"]').text();
      expect(error).toBe('Đã xảy ra lỗi. Vui lòng thử lại.');
      expect(error).not.toContain('private backend detail');
    });

  it('shows the link date and unlinks after confirmation', async () => {
    linked = [identity('id-a', 'google')];
    const wrapper = await mountSettings();
    expect(wrapper.get('[data-identity-id="id-a"]').text()).toContain(
      'Linked on September 18, 2026',
    );
    const before = meCalls;

    await confirmUnlink(wrapper, 'id-a');

    expect(deletes).toEqual([{ id: 'id-a', csrf: 'csrf', body: '' }]);
    expect(meCalls).toBeGreaterThan(before);
    expect(wrapper.find('[data-identity-id="id-a"]').exists()).toBe(false);
    expect(wrapper.find('[data-testid="unlink-error"]').exists()).toBe(false);
  });

  it('offers to sign out other devices after a successful unlink',
    async () => {
      linked = [identity('id-a', 'google')];
      sessionRows = [
        session('s-current', true),
        session('s-other-1', false),
        session('s-other-2', false),
      ];
      const wrapper = await mountSettings();
      expect(wrapper.find('[data-testid="unlink-success"]').exists())
        .toBe(false);

      await confirmUnlink(wrapper, 'id-a');

      const notice = wrapper.get('[data-testid="unlink-success"]');
      expect(notice.text()).toContain(
        'Google is unlinked. Devices that are already signed in stay signed '
        + 'in.',
      );
      await notice.get('[data-testid="unlink-sign-out-others"]')
        .trigger('click');
      await flushPromises();
      await flushPromises();

      await vi.waitFor(() =>
        expect(revokedSessions).toEqual(['s-other-1', 's-other-2']));
      await vi.waitFor(() =>
        expect(wrapper.find('[data-testid="unlink-success"]').exists())
          .toBe(false));
    });

  it('shows no sign-out offer when the identity was already gone',
    async () => {
      linked = [identity('id-a', 'google')];
      sessionRows = [session('s-current', true), session('s-other-1', false)];
      respondToDelete = (event) => {
        linked = [];
        setResponseStatus(event, 404);
        return { error: { code: 'not_found', message: 'x' } };
      };
      const wrapper = await mountSettings();
      await confirmUnlink(wrapper, 'id-a');
      await vi.waitFor(() =>
        expect(wrapper.find('[data-identity-id="id-a"]').exists()).toBe(false));
      expect(wrapper.find('[data-testid="unlink-success"]').exists())
        .toBe(false);
    });

  it('reauthenticates with the password, then retries the unlink', async () => {
    linked = [identity('id-a', 'google')];
    let calls = 0;
    respondToDelete = (event, id) => {
      calls += 1;
      if (calls === 1) {
        setResponseStatus(event, 403);
        return { error: { code: 'reauth_required', message: 'x' } };
      }
      return removeOnDelete(event, id);
    };
    const wrapper = await mountSettings();
    await confirmUnlink(wrapper, 'id-a');

    const form = wrapper.get('[data-testid="unlink-reauth-password"]');
    expect(form.text()).toContain('before unlinking Google');
    await wrapper.get('#unlink-current-password').setValue('correct horse');
    await form.trigger('submit');
    await flushPromises();
    await flushPromises();

    expect(reauthBodies).toEqual([
      JSON.stringify({ password: 'correct horse' }),
    ]);
    expect(deletes.map((d) => d.id)).toEqual(['id-a', 'id-a']);
    expect(wrapper.find('[data-identity-id="id-a"]').exists()).toBe(false);
    expect(
      wrapper.find('[data-testid="unlink-reauth-password"]').exists(),
    ).toBe(false);
  });

  it('translates password visibility name and preserves reauthentication draft',
    async () => {
      linked = [identity('id-a', 'google')];
      respondToDelete = (event) => {
        setResponseStatus(event, 403);
        return { error: { code: 'reauth_required', message: 'x' } };
      };
      const wrapper = await mountSettings(['google'], document.body);
      await confirmUnlink(wrapper, 'id-a');

      const password = wrapper.get('#unlink-current-password');
      await password.setValue('correct horse');
      (password.element as HTMLInputElement).focus();

      useState<'vi' | 'en'>('aboutme-locale').value = 'vi';
      await flushPromises();

      expect(wrapper.get('#unlink-current-password').element).toBe(
        password.element,
      );
      expect((password.element as HTMLInputElement).value)
        .toBe('correct horse');
      expect(document.activeElement).toBe(password.element);
      expect(wrapper.get('[aria-label="Hiện mật khẩu hiện tại"]').exists())
        .toBe(true);
    });

  it('offers provider reauthentication when there is no password',
    async () => {
      hasPassword = false;
      linked = [identity('id-a', 'google'), identity('id-b', 'google')];
      respondToDelete = (event) => {
        setResponseStatus(event, 403);
        return { error: { code: 'reauth_required', message: 'x' } };
      };
      const wrapper = await mountSettings();
      await confirmUnlink(wrapper, 'id-b');

      const prompt = wrapper.get('[data-testid="unlink-reauth-provider"]');
      expect(prompt.findAll('button').map((b) => b.text())).toEqual([
        'Continue with Google',
        'Cancel',
      ]);
    });

  it('explains the last sign-in method refusal', async () => {
    linked = [identity('id-a', 'google')];
    respondToDelete = (event) => {
      setResponseStatus(event, 409);
      return { error: { code: 'last_sign_in_method', message: 'x' } };
    };
    const wrapper = await mountSettings();
    await confirmUnlink(wrapper, 'id-a');

    expect(wrapper.get('[data-testid="unlink-error"]').text()).toBe(
      'Add a password or link another provider before removing this one.',
    );
    expect(wrapper.find('[data-identity-id="id-a"]').exists()).toBe(true);
  });

  it('refreshes quietly when the identity is already gone', async () => {
    linked = [identity('id-a', 'google')];
    respondToDelete = (event) => {
      linked = [];
      setResponseStatus(event, 404);
      return { error: { code: 'not_found', message: 'x' } };
    };
    const wrapper = await mountSettings();
    await confirmUnlink(wrapper, 'id-a');

    await vi.waitFor(() =>
      expect(wrapper.find('[data-identity-id="id-a"]').exists()).toBe(false));
    expect(wrapper.find('[data-testid="unlink-error"]').exists()).toBe(false);
  });

  it('disables Unlink for the only sign-in method', async () => {
    hasPassword = false;
    linked = [identity('id-a', 'google'), identity('id-b', 'github')];
    const wrapper = await mountSettings(['google']);

    const only = unlinkButton(wrapper, 'id-a');
    expect(only.attributes('disabled')).toBeDefined();
    const reason = wrapper.get(
      '[data-identity-id="id-a"] [data-testid="unlink-blocked"]',
    );
    expect(reason.text()).toBe(
      'Add a password or link another provider before removing this one.',
    );
    expect(only.attributes('aria-describedby')).toBe(reason.attributes('id'));
    // The GitHub identity is not a sign-in method, so removing it is allowed.
    expect(unlinkButton(wrapper, 'id-b').attributes('disabled'))
      .toBeUndefined();
  });

  it('keys two identities of one provider by id', async () => {
    hasPassword = false;
    linked = [
      identity('id-a', 'google', '2026-09-01T00:00:00Z'),
      identity('id-b', 'google', '2026-09-18T03:00:00Z'),
    ];
    const wrapper = await mountSettings();
    const rows = wrapper.findAll('[data-testid="linked-provider-google"]');
    expect(rows.map((row) => row.attributes('data-identity-id'))).toEqual([
      'id-a',
      'id-b',
    ]);
    expect(rows[0]!.text()).toContain('Linked on September 1, 2026');
    // Each is the other's fallback, so both can be removed.
    expect(unlinkButton(wrapper, 'id-a').attributes('disabled'))
      .toBeUndefined();

    await confirmUnlink(wrapper, 'id-b');

    expect(deletes.map((d) => d.id)).toEqual(['id-b']);
    expect(
      wrapper.findAll('[data-testid="linked-provider-google"]')
        .map((row) => row.attributes('data-identity-id')),
    ).toEqual(['id-a']);
    expect(unlinkButton(wrapper, 'id-a').attributes('disabled')).toBeDefined();
  });
});
