import { beforeEach, describe, expect, it } from 'vitest';
import {
  mountSuspended,
  registerEndpoint,
} from '@nuxt/test-utils/runtime';
import { setResponseStatus } from 'h3';
import { defineComponent } from 'vue';
import {
  AgentGrantsFailure,
  useAgentGrants,
} from '../app/composables/agentGrants';

const me = {
  data: {
    user: {
      id: 'user-1',
      email: 'ada@example.com',
      name: 'Ada',
      avatarKey: null,
      hasPassword: true,
    },
    csrfToken: 'csrf-initial',
    identities: [],
  },
};

const grant = {
  id: '018f5b6a-9a3e-7c21-8b1e-000000000030',
  clientName: 'Resume assistant',
  scopes: ['resumes:read', 'resumes:write'],
  createdAt: '2026-09-01T09:00:00Z',
  lastUsedAt: null,
};

const Probe = defineComponent({
  setup() {
    return useAgentGrants();
  },
  template: '<div />',
});

beforeEach(() => {
  registerEndpoint('/api/v1/me', () => me);
});

describe('useAgentGrants', () => {
  it('uses the generated GET path and shapes nullable lastUsedAt', async () => {
    let requestURL = '';
    registerEndpoint('/api/v1/me/agents', {
      method: 'GET',
      handler: (event) => {
        requestURL = event.node.req.url ?? '';
        return { data: { grants: [grant] } };
      },
    });

    const wrapper = await mountSuspended(Probe);
    await wrapper.vm.refresh();

    expect(wrapper.vm.grants).toEqual([grant]);
    expect(new URL(requestURL, 'http://localhost').pathname).toContain(
      '/api/v1/me/agents',
    );
  });

  it(
    'replaces grants atomically and rejects malformed success payloads',
    async () => {
      let calls = 0;
      registerEndpoint('/api/v1/me/agents', {
        method: 'GET',
        handler: () => {
          calls += 1;
          return calls === 1
            ? { data: { grants: [grant] } }
            : { data: { grants: [{ ...grant, lastUsedAt: 42 }] } };
        },
      });

      const wrapper = await mountSuspended(Probe);
      await wrapper.vm.refresh();
      await expect(wrapper.vm.refresh()).rejects.toMatchObject({
        kind: 'unavailable',
      });
      expect(wrapper.vm.grants).toEqual([grant]);
    },
  );

  it.each([
    [401, 'session_required', 'session-required'],
    [404, 'not_found', 'not-found'],
    [400, 'session_required', 'unavailable'],
    [401, 'other', 'unavailable'],
    [500, 'not_found', 'unavailable'],
  ] as const)('maps GET %s/%s to %s', async (status, code, kind) => {
    registerEndpoint('/api/v1/me/agents', {
      method: 'GET',
      handler: (event) => {
        setResponseStatus(event, status);
        return { error: { code, message: 'secret server message' } };
      },
    });
    const wrapper = await mountSuspended(Probe);
    await expect(wrapper.vm.refresh()).rejects.toMatchObject({ kind });
  });

  it('uses the exact encoded DELETE path and CSRF header', async () => {
    let requestURL = '';
    let csrf = '';
    registerEndpoint('/api/v1/me/agents/a%2Fb', {
      method: 'DELETE',
      handler: (event) => {
        requestURL = event.node.req.url ?? '';
        csrf = Object.entries(event.node.req.headers).find(
          ([key]) => key.toLowerCase() === 'x-csrf-token',
        )?.[1] ?? '';
        setResponseStatus(event, 204);
        return null;
      },
    });
    const wrapper = await mountSuspended(Probe);
    await expect(wrapper.vm.revoke('a/b')).resolves.toBeUndefined();
    expect(requestURL).toContain('/api/v1/me/agents/a%2Fb');
    expect(csrf).toBe('csrf-initial');
  });

  it.each([
    [401, 'session_required', 'session-required'],
    [404, 'not_found', 'not-found'],
    [403, 'csrf_rejected', 'unavailable'],
    [400, 'other', 'unavailable'],
    [500, 'not_found', 'unavailable'],
  ] as const)('maps DELETE %s/%s to %s', async (status, code, kind) => {
    registerEndpoint('/api/v1/me/agents/grant-1', {
      method: 'DELETE',
      handler: (event) => {
        setResponseStatus(event, status);
        return { error: { code, message: 'secret server message' } };
      },
    });
    const wrapper = await mountSuspended(Probe);
    await expect(wrapper.vm.revoke('grant-1')).rejects.toMatchObject({ kind });
  });

  it('does not retry a general 4xx DELETE', async () => {
    let calls = 0;
    registerEndpoint('/api/v1/me/agents/grant-1', {
      method: 'DELETE',
      handler: (event) => {
        calls += 1;
        setResponseStatus(event, 400);
        return { error: { code: 'other', message: 'x' } };
      },
    });
    const wrapper = await mountSuspended(Probe);
    await expect(wrapper.vm.revoke('grant-1')).rejects.toBeInstanceOf(
      AgentGrantsFailure,
    );
    expect(calls).toBe(1);
  });

  it('retries exact CSRF rejection once through useAuth', async () => {
    let calls = 0;
    registerEndpoint('/api/v1/me/agents/grant-1', {
      method: 'DELETE',
      handler: (event) => {
        calls += 1;
        if (calls === 1) {
          setResponseStatus(event, 403);
          return { error: { code: 'csrf_rejected', message: 'x' } };
        }
        setResponseStatus(event, 204);
        return null;
      },
    });
    const wrapper = await mountSuspended(Probe);
    await expect(wrapper.vm.revoke('grant-1')).resolves.toBeUndefined();
    expect(calls).toBe(2);
  });
});
