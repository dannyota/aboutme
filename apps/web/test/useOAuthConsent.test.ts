import { beforeEach, describe, expect, it } from 'vitest';
import {
  mountSuspended,
  registerEndpoint,
} from '@nuxt/test-utils/runtime';
import { readRawBody, setResponseStatus } from 'h3';
import { defineComponent } from 'vue';
import {
  OAuthConsentFailure,
  useOAuthConsent,
} from '../app/composables/useOAuthConsent';

const query = {
  client_id: '018f5b6a-9a3e-7c21-8b1e-000000000001',
  redirect_uri: 'https://agent.example/callback',
  response_type: 'code' as const,
  scope: 'resumes:read resumes:write' as const,
  state: 'opaque-state',
  code_challenge: 'abcdefghijklmnopqrstuvwxyz1234567890123456789',
  code_challenge_method: 'S256' as const,
};

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

const Probe = defineComponent({
  setup() {
    return useOAuthConsent();
  },
  template: '<div />',
});

beforeEach(() => {
  registerEndpoint('/api/v1/me', () => me);
});

describe('useOAuthConsent', () => {
  it('shapes the generated GET query and returns the response', async () => {
    let requestURL = '';
    registerEndpoint('/api/v1/oauth/consent', {
      method: 'GET',
      handler: (event) => {
        requestURL = event.node.req.url ?? '';
        return {
          data: { clientName: 'Resume agent', scopes: ['resumes:read'] },
        };
      },
    });

    const wrapper = await mountSuspended(Probe);
    const result = await wrapper.vm.get(query);
    expect(result).toEqual({
      clientName: 'Resume agent',
      scopes: ['resumes:read'],
    });
    const params = new URL(requestURL, 'http://localhost').searchParams;
    expect(Object.fromEntries(params)).toEqual(query);
  });

  it.each(['approve', 'deny'] as const)(
    'posts the exact body for %s and returns redirectTo',
    async (decision) => {
      let body = '';
      registerEndpoint('/api/v1/oauth/consent', {
        method: 'POST',
        handler: async (event) => {
          body = (await readRawBody(event)) ?? '';
          return { data: { redirectTo: 'https://agent.example/callback?code=x' } };
        },
      });

      const wrapper = await mountSuspended(Probe);
      const result = await wrapper.vm.decide(query, decision);
      expect(result).toEqual({
        redirectTo: 'https://agent.example/callback?code=x',
      });
      expect(JSON.parse(body)).toEqual({ ...query, decision });
    },
  );

  it.each([
    [400, 'request_invalid', 'invalid-request'],
    [404, 'not_found', 'invalid-request'],
    [401, 'session_required', 'session-required'],
    [400, 'other', 'unavailable'],
    [503, 'server_error', 'unavailable'],
  ] as const)('maps GET %s/%s to %s', async (status, code, kind) => {
    registerEndpoint('/api/v1/oauth/consent', {
      method: 'GET',
      handler: (event) => {
        setResponseStatus(event, status);
        return { error: { code, message: 'secret server message' } };
      },
    });
    const wrapper = await mountSuspended(Probe);
    await expect(wrapper.vm.get(query)).rejects.toMatchObject({ kind });
  });

  it('falls back to unavailable for a malformed response', async () => {
    registerEndpoint('/api/v1/oauth/consent', {
      method: 'GET',
      handler: () => ({ data: { clientName: 42, scopes: ['unknown'] } }),
    });
    const wrapper = await mountSuspended(Probe);
    await expect(wrapper.vm.get(query)).rejects.toMatchObject({
      kind: 'unavailable',
    });
  });

  it.each([
    [400, 'request_invalid', 'invalid-request'],
    [404, 'not_found', 'invalid-request'],
    [401, 'session_required', 'session-required'],
    [500, 'other', 'unavailable'],
  ] as const)('maps mutation %s/%s to %s', async (status, code, kind) => {
    let calls = 0;
    registerEndpoint('/api/v1/oauth/consent', {
      method: 'POST',
      handler: (event) => {
        calls += 1;
        setResponseStatus(event, status);
        return { error: { code, message: 'secret server message' } };
      },
    });
    const wrapper = await mountSuspended(Probe);
    await expect(wrapper.vm.decide(query, 'approve')).rejects.toMatchObject({
      kind,
    });
    expect(calls).toBe(1);
  });

  it('does not retry a general 4xx mutation', async () => {
    let calls = 0;
    registerEndpoint('/api/v1/oauth/consent', {
      method: 'POST',
      handler: (event) => {
        calls += 1;
        setResponseStatus(event, 400);
        return { error: { code: 'request_invalid', message: 'x' } };
      },
    });
    const wrapper = await mountSuspended(Probe);
    await expect(wrapper.vm.decide(query, 'approve')).rejects.toBeInstanceOf(
      OAuthConsentFailure,
    );
    expect(calls).toBe(1);
  });

  it('retries only the established exact csrf rejection once', async () => {
    let calls = 0;
    registerEndpoint('/api/v1/oauth/consent', {
      method: 'POST',
      handler: (event) => {
        calls += 1;
        if (calls === 1) {
          setResponseStatus(event, 403);
          return { error: { code: 'csrf_rejected', message: 'x' } };
        }
        return { data: { redirectTo: 'https://agent.example/callback' } };
      },
    });
    const wrapper = await mountSuspended(Probe);
    await expect(wrapper.vm.decide(query, 'approve')).resolves.toEqual({
      redirectTo: 'https://agent.example/callback',
    });
    expect(calls).toBe(2);
  });
});
