import { beforeEach, describe, expect, it, vi } from 'vitest';
import {
  mockNuxtImport,
  mountSuspended,
  registerEndpoint,
} from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { nextTick } from 'vue';
import { setResponseStatus } from 'h3';
import ConnectedAgents from '../app/components/settings/ConnectedAgents.vue';
import SessionsPage from '../app/pages/app/settings/sessions.vue';
import { registerCapabilities } from './support/capabilities';

registerCapabilities();

mockNuxtImport('navigateTo', () => vi.fn());

const me = {
  data: {
    user: {
      id: 'user-1',
      email: 'ada@example.com',
      name: 'Ada',
      avatarKey: null,
      hasPassword: true,
    },
    csrfToken: 'csrf-token',
    identities: [],
  },
};

const grant = {
  id: 'grant-1',
  clientName: '<img src=x onerror=alert(1)>',
  scopes: ['resumes:read', 'resumes:write'],
  createdAt: '2026-09-01T09:00:00Z',
  lastUsedAt: null,
};

let meHandler: () => unknown = () => me;
let agentHandler: () => unknown = () => ({ data: { grants: [grant] } });
let agentStatus: number | null = null;
let failAgentAfterFirst = false;
let agentRequests = 0;
let deleteHandler: (
  event: Parameters<typeof setResponseStatus>[0],
) => unknown = (event) => {
  setResponseStatus(event, 204);
  return null;
};

registerEndpoint('/api/v1/me', () => meHandler());
registerEndpoint('/api/v1/me/agents', (event) => {
  agentRequests += 1;
  const status = failAgentAfterFirst && agentRequests > 1 ? 401 : agentStatus;
  if (status !== null) setResponseStatus(event, status);
  if (status === 401 && failAgentAfterFirst) {
    return { error: { code: 'session_required', message: 'secret' } };
  }
  return agentHandler();
});
registerEndpoint('/api/v1/me/agents/grant-1', {
  method: 'DELETE',
  handler: (event) => deleteHandler(event),
});

function registerCommon(grants: unknown = [grant]): void {
  registerEndpoint('/api/v1/sessions', () => ({ data: [] }));
  meHandler = () => me;
  agentHandler = () => ({ data: { grants } });
  agentStatus = null;
  failAgentAfterFirst = false;
  agentRequests = 0;
  deleteHandler = (event) => {
    setResponseStatus(event, 204);
    return null;
  };
}

describe('ConnectedAgents', () => {
  beforeEach(() => {
    registerCommon();
    vi.mocked(navigateTo).mockClear();
  });

  it(
    'renders hostile client names as text without ' + 'injecting elements',
    async () => {
      const wrapper = await mountSuspended(ConnectedAgents);
      await flushPromises();
      expect(wrapper.get('[data-testid="connected-agents"] h2').text()).toBe(
        'Connected agents',
      );
      expect(
        wrapper.find('[data-testid="connected-agents"] [as]').exists(),
      ).toBe(false);
      expect(wrapper.text()).toContain(grant.clientName);
      expect(wrapper.find('[onerror]').exists()).toBe(false);
    },
  );

  it(
    'renders closed scopes and deterministic times including ' + 'Never',
    async () => {
      const wrapper = await mountSuspended(ConnectedAgents);
      await flushPromises();
      expect(wrapper.text()).toContain('Read resumes');
      expect(wrapper.text()).toContain('Write resumes');
      expect(wrapper.text()).toContain('Created September 1, 2026');
      expect(wrapper.text()).toContain('Last used Never');
      expect(wrapper.get('[datetime]').attributes('datetime')).toBe(
        grant.createdAt,
      );
    },
  );

  it('explains MCP in the empty state without an external link', async () => {
    agentHandler = () => ({ data: { grants: [] } });
    const wrapper = await mountSuspended(ConnectedAgents);
    await flushPromises();
    expect(wrapper.text()).toContain('connect through MCP');
    expect(wrapper.find('[href]').exists()).toBe(false);
  });

  it(
    'shows fixed loading and unavailable copy with keyboard ' + 'Retry',
    async () => {
      agentHandler = () => ({
        error: { code: 'other', message: 'secret raw error' },
      });
      const wrapper = await mountSuspended(ConnectedAgents);
      expect(wrapper.text()).toContain('Loading connected agents');
      await flushPromises();
      expect(wrapper.text()).toContain('Connected agents are unavailable');
      expect(wrapper.text()).not.toContain('secret raw error');
      expect(
        wrapper.get('[data-testid="agents-retry"]').attributes('data-slot'),
      ).toBe('button');
    },
  );

  it('retries a failed list successfully from the Retry button', async () => {
    agentStatus = 500;
    agentHandler = () => ({ error: { code: 'other', message: 'secret' } });
    const wrapper = await mountSuspended(ConnectedAgents);
    await flushPromises();
    expect(wrapper.get('[data-testid="agents-error"]').exists()).toBe(true);

    agentStatus = null;
    agentHandler = () => ({ data: { grants: [grant] } });
    await wrapper.get('[data-testid="agents-retry"]').trigger('click');
    await flushPromises();
    expect(wrapper.find('[data-testid="agents-error"]').exists()).toBe(false);
    expect(wrapper.find('[data-testid="agent-row"]').exists()).toBe(true);
  });

  it(
    'navigates to login for an exact initial list session ' + 'failure',
    async () => {
      agentStatus = 401;
      agentHandler = () => ({
        error: { code: 'session_required', message: 'secret' },
      });
      await mountSuspended(ConnectedAgents);
      await flushPromises();
      expect(navigateTo).toHaveBeenCalledWith('/login');
    },
  );

  it(
    'opens, cancels, escapes, and returns focus from ' + 'confirmation',
    async () => {
      const wrapper = await mountSuspended(ConnectedAgents, {
        attachTo: document.body,
      });
      await flushPromises();
      const revoke = wrapper.get('[data-testid="agent-revoke"]');
      (revoke.element as HTMLButtonElement).focus();
      await revoke.trigger('click');
      await flushPromises();
      expect(
        document.body.querySelector('[role="alertdialog"]'),
      ).not.toBeNull();
      expect(document.activeElement?.textContent).toContain('Cancel');
      document.body
        .querySelector('[role="alertdialog"]')!
        .dispatchEvent(
          new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }),
        );
      await flushPromises();
      expect(document.body.querySelector('[role="alertdialog"]')).toBeNull();
      expect(document.activeElement).toBe(revoke.element);

      await revoke.trigger('click');
      await flushPromises();
      document.body
        .querySelector<HTMLButtonElement>(
          '[data-action="agent-revoke-cancel"]',
        )!
        .click();
      await flushPromises();
      expect(document.body.querySelector('[role="alertdialog"]')).toBeNull();
      expect(document.activeElement).toBe(revoke.element);
    },
  );

  it(
    'confirms by keyboard, refreshes exactly, and closes a 404 ' + 'quietly',
    async () => {
      let gets = 0;
      let deletes = 0;
      agentHandler = () => {
        gets += 1;
        return { data: { grants: gets === 1 ? [grant] : [] } };
      };
      deleteHandler = (event) => {
        deletes += 1;
        setResponseStatus(event, 404);
        return { error: { code: 'not_found', message: 'raw' } };
      };
      const wrapper = await mountSuspended(ConnectedAgents);
      await flushPromises();
      await wrapper.get('[data-testid="agent-revoke"]').trigger('click');
      document.body
        .querySelector<HTMLButtonElement>(
          '[data-action="agent-revoke-confirm"]',
        )!
        .click();
      await flushPromises();
      await flushPromises();
      await new Promise((resolve) => setTimeout(resolve, 20));
      expect(deletes).toBe(1);
      expect(gets).toBe(2);
      expect(document.body.querySelector('[role="alertdialog"]')).toBeNull();
      expect(wrapper.find('[data-testid="agents-error"]').exists()).toBe(false);
    },
  );

  it('revokes through the confirm dialog and returns focus', async () => {
    const wrapper = await mountSuspended(ConnectedAgents, {
      attachTo: document.body,
    });
    await flushPromises();
    const trigger = wrapper.get('[data-testid="agent-revoke"]');
    (trigger.element as HTMLButtonElement).focus();
    await trigger.trigger('click');
    await nextTick();
    const dialog = document.body.querySelector('[role="alertdialog"]')!;
    expect(dialog.textContent).toContain('Revoke access');
    document.body
      .querySelector<HTMLButtonElement>('[data-action="agent-revoke-confirm"]')!
      .click();
    await flushPromises();
    await new Promise((resolve) => setTimeout(resolve, 250));
    expect(document.body.querySelector('[role="alertdialog"]')).toBeNull();
    expect(document.activeElement).toBe(trigger.element);
    wrapper.unmount();
  });

  it('refreshes exactly once after a successful DELETE', async () => {
    let gets = 0;
    let deletes = 0;
    agentHandler = () => {
      gets += 1;
      return { data: { grants: gets === 1 ? [grant] : [] } };
    };
    deleteHandler = (event) => {
      deletes += 1;
      setResponseStatus(event, 204);
      return null;
    };
    const wrapper = await mountSuspended(ConnectedAgents);
    await flushPromises();
    await wrapper.get('[data-testid="agent-revoke"]').trigger('click');
    await nextTick();
    document.body
      .querySelector<HTMLButtonElement>('[data-action="agent-revoke-confirm"]')!
      .click();
    await flushPromises();
    await flushPromises();
    expect(deletes).toBe(1);
    expect(gets).toBe(2);
    expect(wrapper.find('[data-testid="agent-row"]').exists()).toBe(false);
  });

  it(
    'preserves the row on unavailable revoke and prevents rapid '
    + 'double-submit',
    async () => {
      let deletes = 0;
      deleteHandler = (event) => {
        deletes += 1;
        return new Promise((resolve) => {
          setTimeout(() => {
            setResponseStatus(event, 500);
            resolve({ error: { code: 'other', message: 'raw' } });
          }, 5);
        });
      };
      const wrapper = await mountSuspended(ConnectedAgents);
      await flushPromises();
      await wrapper.get('[data-testid="agent-revoke"]').trigger('click');
      const confirm = document.body.querySelector<HTMLButtonElement>(
        '[data-action="agent-revoke-confirm"]',
      )!;
      confirm.click();
      confirm.click();
      await new Promise((resolve) => setTimeout(resolve, 10));
      await flushPromises();
      await flushPromises();
      expect(deletes).toBe(1);
      expect(wrapper.find('[data-testid="agent-row"]').exists()).toBe(true);
      expect(wrapper.text()).toContain('Connected agents are unavailable');
    },
  );

  it('navigates to login for an exact revoke session failure', async () => {
    deleteHandler = (event) => {
      setResponseStatus(event, 401);
      return { error: { code: 'session_required', message: 'secret' } };
    };
    const wrapper = await mountSuspended(ConnectedAgents);
    await flushPromises();
    await wrapper.get('[data-testid="agent-revoke"]').trigger('click');
    await nextTick();
    document.body
      .querySelector<HTMLButtonElement>('[data-action="agent-revoke-confirm"]')!
      .click();
    await flushPromises();
    await vi.waitFor(() => {
      expect(navigateTo).toHaveBeenCalledWith('/login');
    });
  });

  it(
    'navigates to login when the post-success refresh loses the ' + 'session',
    async () => {
      let deletes = 0;
      agentHandler = () => {
        return { data: { grants: [grant] } };
      };
      deleteHandler = (event) => {
        deletes += 1;
        setResponseStatus(event, 204);
        return null;
      };
      const wrapper = await mountSuspended(ConnectedAgents);
      await flushPromises();
      failAgentAfterFirst = true;
      await wrapper.get('[data-testid="agent-revoke"]').trigger('click');
      document.body
        .querySelector<HTMLButtonElement>(
          '[data-action="agent-revoke-confirm"]',
        )!
        .click();
      await flushPromises();
      await flushPromises();
      expect(deletes).toBe(1);
      expect(agentRequests).toBe(2);
      await vi.waitFor(() => {
        expect(navigateTo).toHaveBeenCalledWith('/login');
      });
    },
  );

  it('integrates after password settings', async () => {
    const wrapper = await mountSuspended(SessionsPage);
    await flushPromises();
    expect(wrapper.text()).toContain('Signed-in devices');
    const agents = wrapper.get('[data-testid="connected-agents"]').element;
    const password = wrapper.get('[data-testid="password-settings"]').element;
    expect(
      password.compareDocumentPosition(agents)
      & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
  });

  it('disables revoke before the CSRF token is available', async () => {
    const wrapper = await mountSuspended(ConnectedAgents);
    await flushPromises();
    meHandler = () => ({
      data: {
        ...me.data,
        csrfToken: null,
      },
    });
    await refreshNuxtData();
    await flushPromises();
    expect(wrapper.vm.csrfToken).toBe(null);
    expect(wrapper.get('[data-testid="agent-revoke"]').element.disabled).toBe(
      true,
    );
  });
});
